package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type branchCreateCmd struct {
	Name string `arg:"" optional:"" help:"Name of the new branch"`

	Insert bool   `help:"Restack the upstack of the target branch onto the new branch"`
	Below  bool   `help:"Place the branch below the target branch and restack its upstack"`
	Target string `short:"t" placeholder:"BRANCH" help:"Branch to create the new branch above/below"`

	All     bool   `short:"a" help:"Automatically stage modified and deleted files"`
	Message string `short:"m" placeholder:"MSG" help:"Commit message"`

	NoVerify bool `help:"Bypass pre-commit and commit-msg hooks."`

	Commit bool `negatable:"" default:"true" config:"branchCreate.commit" help:"Commit staged changes to the new branch, or create an empty commit"`
}

func (*branchCreateCmd) Help() string {
	return text.Dedent(`
		Staged changes will be committed to the new branch.
		If there are no staged changes, an empty commit will be created.
		Use -a/--all to automatically stage modified and deleted files,
		just like 'git commit -a'.
		Use --no-commit to create the branch without committing.

		If a branch name is not provided,
		it will be generated from the commit message.

		The new branch will use the current branch as its base.
		Use --target to specify a different base branch.

		--insert will move the branches upstack from the target branch
		on top of the new branch.
		--below will create the new branch below the target branch.

		For example, given the following stack, with A checked out:

			    ┌── C
			  ┌─┴ B
			┌─┴ A ◀
			trunk

		'gs branch create X' will have the following effects
		with different flags:

			         gs branch create X

			 default  │   --insert   │  --below
			──────────┼──────────────┼──────────
			  ┌── X   │        ┌── C │       ┌── C
			  │ ┌── C │      ┌─┴ B   │     ┌─┴ B
			  ├─┴ B   │    ┌─┴ X     │   ┌─┴ A
			┌─┴ A     │  ┌─┴ A       │ ┌─┴ X
			trunk     │  trunk       │ trunk

		In all cases above, use of -t/--target flag will change the
		target (A) to the specified branch:

			     gs branch create X --target B

			 default  │   --insert   │  --below
			──────────┼──────────────┼────────────
			    ┌── X │        ┌── C │       ┌── C
			    ├── C │      ┌─┴ X   │     ┌─┴ B
			  ┌─┴ B   │    ┌─┴ B     │   ┌─┴ X
			┌─┴ A     │  ┌─┴ A       │ ┌─┴ A
			trunk     │  trunk       │ trunk
	`)
}

func (cmd *branchCreateCmd) Run(ctx context.Context, log *log.Logger, view ui.View) (err error) {
	if cmd.Name == "" && !cmd.Commit {
		return fmt.Errorf("a branch name is required with --no-commit")
	}

	repo, store, svc, err := openRepo(ctx, log, view)
	if err != nil {
		return err
	}
	trunk := store.Trunk()

	if cmd.Target == "" {
		cmd.Target, err = repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	// If a branch name was specified, verify it's unused.
	// We do this before any changes to the working tree or index.
	if cmd.Name != "" {
		if _, err := repo.PeelToCommit(ctx, cmd.Name); err == nil {
			return fmt.Errorf("branch already exists: %v", cmd.Name)
		}
	}

	baseName := cmd.Target
	var (
		baseHash       git.Hash
		restackOntoNew []string // branches to restack onto the new branch
	)
	if cmd.Below {
		if cmd.Target == trunk {
			log.Error("--below: cannot create a branch below trunk")
			return fmt.Errorf("--below cannot be used from %v", trunk)
		}

		b, err := svc.LookupBranch(ctx, cmd.Target)
		if err != nil {
			return fmt.Errorf("branch not tracked: %v", cmd.Target)
		}

		// If trying to insert below the target branch,
		// we'll detach to *its* base branch,
		// and restack the base branch onwards.
		restackOntoNew = append(restackOntoNew, cmd.Target)
		baseName = b.Base
		baseHash = b.BaseHash
	} else if cmd.Insert {
		// If inserting, above the target branch,
		// restack all its upstack branches on top of the new branch.
		aboves, err := svc.ListAbove(ctx, cmd.Target)
		if err != nil {
			return fmt.Errorf("list branches above %s: %w", cmd.Target, err)
		}

		restackOntoNew = append(restackOntoNew, aboves...)
	}

	if baseHash == "" || baseHash.IsZero() {
		baseHash, err = repo.PeelToCommit(ctx, baseName)
		if err != nil {
			return fmt.Errorf("resolve %v: %w", baseName, err)
		}
	}

	var branchCreated bool // set only after CreateBranch
	branchAt := baseHash
	if cmd.Commit {
		commitHash, restore, err := cmd.commit(ctx, repo, baseName, log)
		if err != nil {
			return err
		}
		branchAt = commitHash

		// Staged changes are committed to commitHash.
		// From this point on, to prevent data loss,
		// we'll want to revert to original branch while keeping the changes
		// if we failed to create the new branch for any reason.
		//
		// The condition for this is not whether an error is returned,
		// but whether the new branch was successfully created.
		defer func() {
			if branchCreated {
				return
			}

			log.Warn("Unable to create branch. Rolling back.",
				"branch", cmd.Target)

			if restoreErr := restore(); restoreErr != nil {
				log.Error("Could not roll back. You may need to reset manually.", "error", restoreErr)
				log.Errorf("Get your changes from: %s", commitHash)
			}
		}()

		if cmd.Name == "" {
			// Branch name was not specified.
			// Generate one from the commit message.
			subject, err := repo.CommitSubject(ctx, commitHash.String())
			if err != nil {
				return fmt.Errorf("get commit subject: %w", err)
			}

			name := spice.GenerateBranchName(subject)
			current := name

			// If the auto-generated branch name already exists,
			// append a number to it until we find an unused name.
			_, err = repo.PeelToCommit(ctx, current)
			for num := 2; err == nil; num++ {
				current = fmt.Sprintf("%s-%d", name, num)
				_, err = repo.PeelToCommit(ctx, current)
			}

			cmd.Name = current
		}
	}

	// Start the transaction and make sure it would work
	// before actually creating the branch.
	// This way, if the transaction would've failed anyway
	// (e.g. because of a cycle or an untracked base branch)
	// then we won't commit any changes to the new branch
	// and rollback to the original branch and staged changes.
	branchTx := store.BeginBranchTx()
	if err := branchTx.Upsert(ctx, state.UpsertRequest{
		Name:     cmd.Name,
		Base:     baseName,
		BaseHash: baseHash,
	}); err != nil {
		return fmt.Errorf("add branch %v with base %v: %w", cmd.Name, baseName, err)
	}

	for _, branch := range restackOntoNew {
		// For --insert and --below, set the base branch of all affected
		// branches to the newly created branch.
		//
		// We'll run a restack command after this to update the state.
		if err := branchTx.Upsert(ctx, state.UpsertRequest{
			Name: branch,
			Base: cmd.Name,
		}); err != nil {
			return fmt.Errorf("update base branch of %v: %w", branch, err)
		}
	}

	if err := repo.CreateBranch(ctx, git.CreateBranchRequest{
		Name: cmd.Name,
		Head: branchAt.String(),
	}); err != nil {
		return fmt.Errorf("create branch: %w", err)
	}

	branchCreated = true
	if err := repo.Checkout(ctx, cmd.Name); err != nil {
		return fmt.Errorf("checkout branch: %w", err)
	}

	var msg string
	switch {
	case cmd.Below:
		msg = fmt.Sprintf("insert branch %s below %s", cmd.Name, cmd.Target)
	case cmd.Insert:
		msg = fmt.Sprintf("insert branch %s above %s", cmd.Name, cmd.Target)
	default:
		msg = fmt.Sprintf("create branch %s", cmd.Name)
	}

	if err := branchTx.Commit(ctx, msg); err != nil {
		return fmt.Errorf("update branch state: %w", err)
	}

	if cmd.Below || cmd.Insert {
		return (&upstackRestackCmd{}).Run(ctx, log, view)
	}

	return nil
}

// commit commits the staged changes to a detached HEAD
// and returns the hash of the commit.
//
// It also returns a function that can be used to restore
// the repository to its original state if an error occurs.
func (cmd *branchCreateCmd) commit(
	ctx context.Context,
	repo *git.Repository,
	baseName string,
	log *log.Logger,
) (commitHash git.Hash, restore func() error, err error) {
	// We'll need --allow-empty if there are no staged changes.
	diff, err := repo.DiffIndex(ctx, "HEAD")
	if err != nil {
		return "", nil, fmt.Errorf("diff index: %w", err)
	}

	if err := repo.DetachHead(ctx, baseName); err != nil {
		return "", nil, fmt.Errorf("detach head: %w", err)
	}

	if err := repo.Commit(ctx, git.CommitRequest{
		AllowEmpty: len(diff) == 0,
		Message:    cmd.Message,
		NoVerify:   cmd.NoVerify,
		All:        cmd.All,
	}); err != nil {
		if err := repo.Checkout(ctx, baseName); err != nil {
			log.Warn("Could not restore original branch. You may need to reset manually.", "error", err)
		}
		return "", nil, fmt.Errorf("commit: %w", err)
	}

	commitHash, err = repo.Head(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("get commit hash: %w", err)
	}

	return commitHash, func() error {
		// Move HEAD to the state just before the commit
		// while leaving the index and working tree as-is.
		err := repo.Reset(ctx, commitHash.String()+"^", git.ResetOptions{
			Mode:  git.ResetSoft,
			Quiet: true,
		})
		if err != nil {
			return fmt.Errorf("reset to parent commit: %w", err)
		}

		return repo.Checkout(ctx, baseName)
	}, nil
}

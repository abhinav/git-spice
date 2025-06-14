package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchCreateConfig struct {
	Prefix string `default:"" config:"branchCreate.prefix" help:"Prepend a prefix to the name of the branch being created" hidden:""`
}

type branchCreateCmd struct {
	branchCreateConfig

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

func (cmd *branchCreateCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
) (err error) {
	if cmd.Name == "" && !cmd.Commit {
		return errors.New("a branch name is required with --no-commit")
	}

	trunk := store.Trunk()

	if cmd.Target == "" {
		cmd.Target, err = wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	// If a branch name was specified, verify it's unused.
	// We do this before any changes to the working tree or index.
	if cmd.Name != "" {
		if repo.BranchExists(ctx, cmd.Name) {
			return fmt.Errorf("branch already exists: %v", cmd.Name)
		}
	}

	baseName := cmd.Target
	var (
		baseHash     git.Hash
		stackOntoNew []string // branches to stack onto the new branch

		// Downstack history for the new branch
		// and for those restacked on top of it.
		newMergedDownstack       *[]json.RawMessage
		restackedMergedDownstack *[]json.RawMessage
	)
	if cmd.Below {
		if cmd.Target == trunk {
			return fmt.Errorf("--below cannot be used from %v", trunk)
		}

		b, err := svc.LookupBranch(ctx, cmd.Target)
		if err != nil {
			return fmt.Errorf("branch not tracked: %v", cmd.Target)
		}

		// If trying to insert below the target branch,
		// we'll detach to *its* base branch,
		// and restack the base branch onwards.
		stackOntoNew = append(stackOntoNew, cmd.Target)
		baseName = b.Base
		baseHash = b.BaseHash

		// If the branch is at the bottom of the stack
		// and has a merged downstack history,
		// transfer it to the new branch.
		if len(b.MergedDownstack) > 0 {
			newMergedDownstack = &b.MergedDownstack
			restackedMergedDownstack = new([]json.RawMessage)
		}

		// TODO: Maybe this transfer should take place at submit time?
	} else if cmd.Insert {
		// If inserting, above the target branch,
		// restack all its upstack branches on top of the new branch.
		aboves, err := svc.ListAbove(ctx, cmd.Target)
		if err != nil {
			return fmt.Errorf("list branches above %s: %w", cmd.Target, err)
		}

		stackOntoNew = append(stackOntoNew, aboves...)
	}

	if baseHash == "" || baseHash.IsZero() {
		baseHash, err = repo.PeelToCommit(ctx, baseName)
		if err != nil {
			return fmt.Errorf("resolve %v: %w", baseName, err)
		}
	}

	var (
		branchCreated bool // set only after CreateBranch
		generatedName bool // set if the branch name was generated
	)
	branchAt := baseHash
	if cmd.Commit {
		commitHash, restore, err := cmd.commit(ctx, wt, baseName, log)
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

			msgName := spice.GenerateBranchName(subject)
			current := cmd.Prefix + msgName

			// If the auto-generated branch name already exists,
			// append a number to it until we find an unused name.
			for num := 2; repo.BranchExists(ctx, current); num++ {
				current = fmt.Sprintf("%s%s-%d", cmd.Prefix, msgName, num)
			}

			cmd.Name = current
			generatedName = true
			log.Debug("Branch name generated from commit",
				"name", cmd.Name, "commit", commitHash)
		}
	}

	branchName := cmd.Name
	if !generatedName {
		// Branch generation already took prefix into account.
		branchName = cmd.Prefix + cmd.Name
	}

	// Start the transaction and make sure it would work
	// before actually creating the branch.
	// This way, if the transaction would've failed anyway
	// (e.g. because of a cycle or an untracked base branch)
	// then we won't commit any changes to the new branch
	// and rollback to the original branch and staged changes.
	branchTx := store.BeginBranchTx()
	if err := branchTx.Upsert(ctx, state.UpsertRequest{
		Name:            branchName,
		Base:            baseName,
		BaseHash:        baseHash,
		MergedDownstack: newMergedDownstack,
	}); err != nil {
		return fmt.Errorf("add branch %v with base %v: %w", branchName, baseName, err)
	}

	for _, branch := range stackOntoNew {
		// For --insert and --below, set the base branch of all affected
		// branches to the newly created branch.
		//
		// We'll run a restack command after this to update the state.
		if err := branchTx.Upsert(ctx, state.UpsertRequest{
			Name:            branch,
			Base:            branchName,
			MergedDownstack: restackedMergedDownstack,
		}); err != nil {
			return fmt.Errorf("update base branch of %v: %w", branch, err)
		}
		log.Debug("Changing branch base", "name", branch, "newBase", branchName)
	}

	if err := repo.CreateBranch(ctx, git.CreateBranchRequest{
		Name: branchName,
		Head: branchAt.String(),
	}); err != nil {
		return fmt.Errorf("create branch: %w", err)
	}

	branchCreated = true
	if err := wt.Checkout(ctx, branchName); err != nil {
		return fmt.Errorf("checkout branch: %w", err)
	}

	var msg string
	switch {
	case cmd.Below:
		msg = fmt.Sprintf("insert branch %s below %s", branchName, cmd.Target)
	case cmd.Insert:
		msg = fmt.Sprintf("insert branch %s above %s", branchName, cmd.Target)
	default:
		msg = "create branch " + branchName
	}

	if err := branchTx.Commit(ctx, msg); err != nil {
		return fmt.Errorf("update branch state: %w", err)
	}

	if cmd.Below || cmd.Insert {
		return (&upstackRestackCmd{}).Run(
			ctx, log, wt, store, svc,
		)
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
	wt *git.Worktree,
	baseName string,
	log *silog.Logger,
) (commitHash git.Hash, restore func() error, err error) {
	// We'll need --allow-empty if there are no staged changes.
	diff, err := wt.DiffIndex(ctx, "HEAD")
	if err != nil {
		return "", nil, fmt.Errorf("diff index: %w", err)
	}

	if err := wt.DetachHead(ctx, baseName); err != nil {
		return "", nil, fmt.Errorf("detach head: %w", err)
	}

	if err := wt.Commit(ctx, git.CommitRequest{
		AllowEmpty: len(diff) == 0,
		Message:    cmd.Message,
		NoVerify:   cmd.NoVerify,
		All:        cmd.All,
	}); err != nil {
		if err := wt.Checkout(ctx, baseName); err != nil {
			log.Warn("Could not restore original branch. You may need to reset manually.", "error", err)
		}
		return "", nil, fmt.Errorf("commit: %w", err)
	}

	commitHash, err = wt.Head(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("get commit hash: %w", err)
	}

	return commitHash, func() error {
		// Move HEAD to the state just before the commit
		// while leaving the index and working tree as-is.
		err := wt.Reset(ctx, commitHash.String()+"^", git.ResetOptions{
			Mode:  git.ResetSoft,
			Quiet: true,
		})
		if err != nil {
			return fmt.Errorf("reset to parent commit: %w", err)
		}

		return wt.Checkout(ctx, baseName)
	}, nil
}

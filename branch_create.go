package gitspice

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/git-spice/internal/git"
	"go.abhg.dev/git-spice/internal/spice"
	"go.abhg.dev/git-spice/internal/state"
	"go.abhg.dev/git-spice/internal/text"
)

type branchCreateCmd struct {
	Name string `arg:"" optional:"" help:"Name of the new branch"`

	Insert bool `help:"Restack the upstack of the current branch on top of the new branch"`
	Below  bool `help:"Place the branch below the current branch. Implies --insert."`

	Message string `short:"m" long:"message" optional:"" help:"Commit message"`
}

func (*branchCreateCmd) Help() string {
	return text.Dedent(`
		Creates a new branch containing the staged changes
		on top of the current branch.
		If there are no staged changes, creates an empty commit.

		By default, the new branch is created on top of the current branch,
		but it does not affect the rest of the stack.
		Use the --insert flag to restack all existing upstack branches
		on top of the new branch.
		For example,

			# trunk -> A -> B -> C
			git checkout A
			gs branch create --insert X
			# trunk -> A -> X -> B -> C

		Instead of --insert,
		you can use --below to place the new branch
		below the current branch.
		This is equivalent to checking out the base branch
		and creating a new branch with --insert there.

			# trunk -> A -> B -> C
			git checkout A
			gs branch create --below X
			# trunk -> X -> A -> B -> C
	`)
}

func (cmd *branchCreateCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) (err error) {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}
	trunk := store.Trunk()

	svc := spice.NewService(repo, store, log)

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	currentHash, err := repo.PeelToCommit(ctx, "HEAD")
	if err != nil {
		return fmt.Errorf("peel to tree: %w", err)
	}

	diff, err := repo.DiffIndex(ctx, currentHash.String())
	if err != nil {
		return fmt.Errorf("diff index: %w", err)
	}

	baseName := currentBranch
	baseHash := currentHash

	// Branches to restack on top of new branch.
	var restackOntoNew []string
	if cmd.Below {
		if currentBranch == trunk {
			log.Error("--below: cannot create a branch below trunk")
			return fmt.Errorf("--below cannot be used from  %v", trunk)
		}

		b, err := svc.LookupBranch(ctx, currentBranch)
		if err != nil {
			return fmt.Errorf("branch not tracked: %v", currentBranch)
		}

		// If trying to insert below current branch,
		// detach to base instead,
		// and restack current branch on top.
		baseName = b.Base
		baseHash = b.BaseHash
		restackOntoNew = append(restackOntoNew, currentBranch)
	} else if cmd.Insert {
		// If inserting, restacking all the upstacks of current branch
		// onto the new branch.
		aboves, err := svc.ListAbove(ctx, currentBranch)
		if err != nil {
			return fmt.Errorf("list branches above %s: %w", currentBranch, err)
		}

		restackOntoNew = append(restackOntoNew, aboves...)
	}

	if err := repo.DetachHead(ctx, baseName); err != nil {
		return fmt.Errorf("detach head: %w", err)
	}
	// From this point on, if there's an error,
	// restore the original branch.
	defer func() {
		if err != nil {
			err = errors.Join(err, repo.Checkout(ctx, currentBranch))
		}
	}()

	if err := repo.Commit(ctx, git.CommitRequest{
		AllowEmpty: len(diff) == 0,
		Message:    cmd.Message,
	}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// Branch name was not specified.
	// Generate one from the commit message.
	if cmd.Name == "" {
		subject, err := repo.CommitSubject(ctx, "HEAD")
		if err != nil {
			return fmt.Errorf("get commit subject: %w", err)
		}

		cmd.Name = spice.GenerateBranchName(subject)
	}

	if err := repo.CreateBranch(ctx, git.CreateBranchRequest{
		Name: cmd.Name,
		Head: "HEAD",
	}); err != nil {
		return fmt.Errorf("create branch: %w", err)
	}

	if err := repo.Checkout(ctx, cmd.Name); err != nil {
		return fmt.Errorf("checkout branch: %w", err)
	}

	var upserts []state.UpsertRequest
	upserts = append(upserts, state.UpsertRequest{
		Name:     cmd.Name,
		Base:     baseName,
		BaseHash: baseHash,
	})

	for _, branch := range restackOntoNew {
		// For --insert and --below, set the base branch of all affected
		// branches to the newly created branch and run a restack.
		upserts = append(upserts, state.UpsertRequest{
			Name: branch,
			Base: cmd.Name,
		})
	}

	var msg string
	switch {
	case cmd.Below:
		msg = fmt.Sprintf("insert branch %s below %s", cmd.Name, baseName)
	case cmd.Insert:
		msg = fmt.Sprintf("insert branch %s above %s", cmd.Name, baseName)
	default:
		msg = fmt.Sprintf("create branch %s", cmd.Name)
	}

	if err := store.Update(ctx, &state.UpdateRequest{
		Upserts: upserts,
		Message: msg,
	}); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	if cmd.Below || cmd.Insert {
		return (&upstackRestackCmd{}).Run(ctx, log, opts)
	}

	return nil
}

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchCreateCmd struct {
	Name string `arg:"" optional:"" help:"Name of the new branch"`

	Insert bool   `help:"Restack the upstack of the current branch on top of the new branch"`
	Below  bool   `help:"Place the branch below the current branch. Implies --insert."`
	Target string `short:"t" help:"Branch to create the new branch above/below"`

	Message string `short:"m" long:"message" optional:"" help:"Commit message"`
}

func (*branchCreateCmd) Help() string {
	return text.Dedent(`
		Creates a new branch containing the staged changes
		on top of the current branch, or --target if specified.
		If there are no staged changes, creates an empty commit.

		By default, the new branch is created on top of the target,
		but it does not affect the rest of the stack.
		Use the --insert flag to move the upstack branches of the
		target onto the new branch.
		Alternatively, use the --below flag to place the new branch
		below the target branch, making the new branch the base of the
		rest of the stack.

		For example, given the following stack, with A checked out:

			    ┌── C
			  ┌─┴ B
			┌─┴ A ◀
			trunk

		'gs branch create X' will create a new branch X on top of A
		and leave B and C unchanged:

			# gs branch create X
			  ┌── X
			  │ ┌── C
			  ├─┴ B
			┌─┴ A
			trunk

		'gs branch create --insert X' will create a new branch X on top
		of A, and move B and C on top of X:

			# gs branch create --insert X
			      ┌── C
			    ┌─┴ B
			  ┌─┴ X
			┌─┴ A
			trunk

		'gs branch create --below X' will create a new branch X below A,
		and move A, B, and C on top of X:

			# gs branch create --below X
			      ┌── C
			    ┌─┴ B
			  ┌─┴ A
			┌─┴ X
			trunk

		In all cases above, use of -t/--target flag will change the
		target (A) to the specified branch:

			# gs branch create --target B X
			      ┌── C
			    ┌─┴ B
			  ┌─┴ X
			┌─┴ A
			trunk
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

	if cmd.Target == "" {
		cmd.Target, err = repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	diff, err := repo.DiffIndex(ctx, "HEAD")
	if err != nil {
		return fmt.Errorf("diff index: %w", err)
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

	if err := repo.DetachHead(ctx, baseName); err != nil {
		return fmt.Errorf("detach head: %w", err)
	}
	// From this point on, if there's an error,
	// restore the original branch.
	defer func() {
		if err != nil {
			err = errors.Join(err, repo.Checkout(ctx, cmd.Target))
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
		msg = fmt.Sprintf("insert branch %s below %s", cmd.Name, cmd.Target)
	case cmd.Insert:
		msg = fmt.Sprintf("insert branch %s above %s", cmd.Name, cmd.Target)
	default:
		msg = fmt.Sprintf("create branch %s", cmd.Name)
	}

	if err := store.UpdateBranch(ctx, &state.UpdateRequest{
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

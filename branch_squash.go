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
	"go.abhg.dev/gs/internal/ui"
)

type branchSquashCmd struct {
	Message string `short:"m" placeholder:"MSG" help:"Use the given message as the commit message."`
}

func (*branchSquashCmd) Help() string {
	return text.Dedent(`
		Squash all commits in the current branch into a single commit
		and restack upstack branches.
	`)
}

func (cmd *branchSquashCmd) Run(
	ctx context.Context,
	log *log.Logger,
	view ui.View,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
) error {
	branchName, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	branch, err := svc.LookupBranch(ctx, branchName)
	if err != nil {
		return fmt.Errorf("lookup branch %q: %w", branchName, err)
	}

	if err := svc.VerifyRestacked(ctx, branchName); err != nil {
		var restackErr *spice.BranchNeedsRestackError
		if errors.As(err, &restackErr) {
			return fmt.Errorf("branch %v needs to be restacked before it can be squashed", branchName)
		}
		return fmt.Errorf("verify restacked: %w", err)
	}

	commitMsg := cmd.Message

	if commitMsg == "" {
		commitMessages, err := repo.CommitMessageRange(ctx, branch.Head.String(), branch.BaseHash.String())
		if err != nil {
			return fmt.Errorf("get commit messages: %w", err)
		}
		commitMsg = commitMessages[len(commitMessages)-1].String()
	}

	// Checkout the branch in detached mode
	if err := (&branchCheckoutCmd{
		Branch: branchName,
		checkoutOptions: checkoutOptions{
			Detach: true,
		},
	}).Run(ctx, log, view, repo, store, svc); err != nil {
		return err
	}

	// Reset the detached branch to the base commit
	if err := repo.Reset(ctx, branch.BaseHash.String(), git.ResetOptions{Mode: git.ResetSoft}); err != nil {
		return err
	}

	// Commit the changes
	if err := repo.Commit(ctx, git.CommitRequest{Message: commitMsg}); err != nil {
		return err
	}

	// Replace the HEAD ref of `branchName` with the new commit
	headHash, err := repo.Head(ctx)
	if err != nil {
		return err
	}

	if err := repo.SetRef(ctx, git.SetRefRequest{
		Ref:  "refs/heads/" + branchName,
		Hash: headHash,
	}); err != nil {
		return err
	}

	// Check out the original branch
	if err := repo.Checkout(ctx, branchName); err != nil {
		return err
	}

	return (&upstackRestackCmd{}).Run(ctx, log, repo, store, svc)
}

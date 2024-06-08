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

type branchOntoCmd struct {
	Branch string `help:"Branch to move" placeholder:"NAME" predictor:"trackedBranches"`
	Onto   string `arg:"" optional:"" help:"Destination branch" predictor:"trackedBranches"`
}

func (*branchOntoCmd) Help() string {
	return text.Dedent(`
		Transplants the commits of a branch on top of another branch
		leaving the rest of the branch stack untouched.
		Use this to extract a single branch from an otherwise unrelated
		branch stack.

		For example,

			# Given:
			#  trunk
			#   └─A
			#     └─B
			#       └─C
			git checkout B
			gs branch onto main
			# Result:
			#  trunk
			#   ├─B
			#   └─A
			#     └─C
	`)
}

func (cmd *branchOntoCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
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

	svc := spice.NewService(repo, store, log)

	if cmd.Branch == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}
	if cmd.Branch == store.Trunk() {
		return fmt.Errorf("cannot move trunk")
	}

	branch, err := svc.LookupBranch(ctx, cmd.Branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return fmt.Errorf("branch not tracked: %s", cmd.Branch)
		}
		return fmt.Errorf("get branch: %w", err)
	}

	if cmd.Onto == "" {
		if !opts.Prompt {
			return fmt.Errorf("cannot proceed without a destination branch: %w", errNoPrompt)
		}

		cmd.Onto, err = (&branchPrompt{
			Exclude:     []string{cmd.Branch},
			TrackedOnly: true,
			Default:     branch.Base,
			Title:       "Select a branch to move onto",
			Description: fmt.Sprintf("Moving %s onto another branch", cmd.Branch),
		}).Run(ctx, repo, store)
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}
	}

	ontoHash, err := repo.PeelToCommit(ctx, cmd.Onto)
	if err != nil {
		return fmt.Errorf("resolve %v: %w", cmd.Onto, err)
	}

	if branch.Base == cmd.Onto {
		log.Infof("%s: already on %s", cmd.Branch, cmd.Onto)
		return nil
	}

	// Onto must be tracked if it's not trunk.
	if cmd.Onto != store.Trunk() {
		if _, err := svc.LookupBranch(ctx, cmd.Onto); err != nil {
			if errors.Is(err, state.ErrNotExist) {
				return fmt.Errorf("branch not tracked: %s", cmd.Onto)
			}
			return fmt.Errorf("get branch: %w", err)
		}
	}

	// To do this operation successfully, we need to:
	// Rebase the branch onto the destination branch.
	// Following that, we need to graft the branch's upstack
	// onto its original base.
	if err := repo.Rebase(ctx, git.RebaseRequest{
		Branch:    cmd.Branch,
		Upstream:  branch.BaseHash.String(),
		Onto:      cmd.Onto,
		Autostash: true,
		Quiet:     true,
	}); err != nil {
		// If the rebase is interrupted,
		// we'll just re-run this command again later.
		return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
			Err:     err,
			Command: []string{"branch", "onto", cmd.Onto},
			Branch:  cmd.Branch,
			Message: fmt.Sprintf("interrupted: %s: branch onto %s", cmd.Branch, cmd.Onto),
		})
	}

	// As long as there are any branches above this one,
	// they need to be grafted onto this branch's original base.
	// We won't update state for this branch until that's done.
	//
	// However, this move operation will be an 'upstack onto'
	// as for each of these branches, we want to keep *their* upstacks.
	aboves, err := svc.ListAbove(ctx, cmd.Branch)
	if err != nil {
		return fmt.Errorf("list branches above %s: %w", cmd.Branch, err)
	}
	for _, above := range aboves {
		if err := (&upstackOntoCmd{
			Branch: above,
			Onto:   branch.Base,
		}).Run(ctx, log, opts); err != nil {
			return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
				Err:     err,
				Command: []string{"branch", "onto", cmd.Onto},
				Branch:  cmd.Branch,
				Message: fmt.Sprintf("interrupted: %s: branch onto %s", cmd.Branch, cmd.Onto),
			})
		}
	}

	// Once all the upstack branches have been grafted onto the original base,
	// we can update the branch state to point to the new base.
	err = store.UpdateBranch(ctx, &state.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{
				Name:     cmd.Branch,
				Base:     cmd.Onto,
				BaseHash: ontoHash,
			},
		},
		Message: fmt.Sprintf("%s: branch onto %s", cmd.Branch, cmd.Onto),
	})
	if err != nil {
		return fmt.Errorf("update store: %w", err)
	}

	return repo.Checkout(ctx, cmd.Branch)
}

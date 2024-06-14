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

type upstackOntoCmd struct {
	Branch string `help:"Branch to start at" placeholder:"NAME" predictor:"trackedBranches"`
	Onto   string `arg:"" optional:"" help:"Destination branch" predictor:"trackedBranches"`
}

// TODO: is 'upstack onto' just 'branch onto' followed by 'upstack restack'?

func (*upstackOntoCmd) Help() string {
	return text.Dedent(`
		Moves a branch and its upstack branches onto another branch.
		Use this to move a complete part of your branch stack to a
		different base.

		For example,

			# Given:
			#  trunk
			#   └─A
			#     └─B
			#       └─C
			git checkout B
			gs upstack onto main
			# Result:
			#  trunk
			#   ├─A
			#   └─B
			#     └─C
	`)
}

func (cmd *upstackOntoCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, store, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

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
			Description: fmt.Sprintf("Moving the upstack of %s onto another branch", cmd.Branch),
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

	// Implementation note:
	// This is a pretty straightforward operation despite the large scope.
	// It starts by rebasing only the current branch onto the target
	// branch, updating internal state to point to the new base.
	// Following that, an 'upstack restack' will handle the upstack branches.
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
			Command: []string{"upstack", "onto", cmd.Onto},
			Branch:  cmd.Branch,
			Message: fmt.Sprintf("interrupted: %s: upstack onto %s", cmd.Branch, cmd.Onto),
		})
	}

	err = store.UpdateBranch(ctx, &state.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{
				Name:     cmd.Branch,
				Base:     cmd.Onto,
				BaseHash: ontoHash,
			},
		},
		Message: fmt.Sprintf("%s: upstack onto %s", cmd.Branch, cmd.Onto),
	})
	if err != nil {
		return fmt.Errorf("update store: %w", err)
	}

	return (&upstackRestackCmd{}).Run(ctx, log, opts)
}

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

type branchDeleteCmd struct {
	Name  string `arg:"" optional:"" help:"Name of the branch to delete" predictor:"branches"`
	Force bool   `short:"f" help:"Force deletion of the branch"`
}

func (*branchDeleteCmd) Help() string {
	return text.Dedent(`
		Deletes the specified branch and removes its changes from the
		stack. Branches above the deleted branch are rebased onto the
		branch's base.

		If a branch name is not provided, an interactive prompt will be
		shown to pick one.
	`)
}

func (cmd *branchDeleteCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
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

	if cmd.Name == "" {
		// If a branch name is not given, prompt for one;
		// assuming we're in interactive mode.
		if !opts.Prompt {
			return fmt.Errorf("cannot proceed without branch name: %w", errNoPrompt)
		}

		cmd.Name, err = (&branchPrompt{
			Exclude:           []string{store.Trunk()},
			ExcludeCheckedOut: true,
			Title:             "Select a branch to delete",
		}).Run(ctx, repo, store)
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	tracked, exists := true, true
	var head git.Hash
	base := store.Trunk()
	if b, err := svc.LookupBranch(ctx, cmd.Name); err != nil {
		if delErr := new(spice.DeletedBranchError); errors.As(err, &delErr) {
			exists = false
			log.Info("branch has already been deleted", "branch", cmd.Name)
		} else if errors.Is(err, state.ErrNotExist) {
			tracked = false
			log.Info("branch is not tracked: deleting anyway", "branch", cmd.Name)
		} else {
			return fmt.Errorf("lookup branch %v: %w", cmd.Name, err)
		}
	} else {
		head = b.Head
		base = b.Base
	}

	// Move to the base of the branch
	// if we're on the branch we're deleting.
	if cmd.Name == currentBranch {
		if err := repo.Checkout(ctx, base); err != nil {
			return fmt.Errorf("checkout %v: %w", base, err)
		}
	}

	// If this branch is tracked,
	// move the the branches above this one to its base
	// without including its own changes.
	//
	// Only then will we update internal state.
	if tracked {
		aboves, err := svc.ListAbove(ctx, cmd.Name)
		if err != nil {
			return fmt.Errorf("list above %v: %w", cmd.Name, err)
		}

		for _, above := range aboves {
			if err := (&upstackOntoCmd{
				Branch: above,
				Onto:   base,
			}).Run(ctx, log, opts); err != nil {
				return fmt.Errorf("move %s onto %s: %w", above, base, err)
			}
		}

		defer func() {
			if err := svc.ForgetBranch(ctx, cmd.Name); err != nil {
				log.Warn("Could not remove branch from state",
					"branch", cmd.Name,
					"err", err)
			}
		}()
	}

	if exists {
		opts := git.BranchDeleteOptions{Force: cmd.Force}
		if err := repo.DeleteBranch(ctx, cmd.Name, opts); err != nil {
			// If the branch still exists,
			// it's likely because it's not merged.
			if _, peelErr := repo.PeelToCommit(ctx, cmd.Name); peelErr == nil {
				log.Error("git refused to delete the branch", "err", err)
				log.Error("try re-running with --force")
				return errors.New("branch not deleted")
			}

			// If the branch doesn't exist,
			// it may already have been deleted.
			log.Warn("branch may already have been deleted", "err", err)
		}

		log.Infof("%v: deleted (was %v)", cmd.Name, head.Short())
	}

	return nil
}

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

		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			currentBranch = ""
		}

		cmd.Name, err = (&branchPrompt{
			Exclude: []string{store.Trunk()},
			Default: currentBranch,
			Title:   "Select a branch to delete",
		}).Run(ctx, repo, store)
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}
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
			log.Debug("branch is not tracked", "error", err)
			log.Info("branch is not tracked: deleting anyway", "branch", cmd.Name)
		} else {
			return fmt.Errorf("lookup branch %v: %w", cmd.Name, err)
		}
	} else {
		head = b.Head
		base = b.Base
	}

	if exists && head == "" {
		hash, err := repo.PeelToCommit(ctx, cmd.Name)
		if err != nil {
			return fmt.Errorf("peel to commit: %w", err)
		}
		head = hash
	}

	// upstack restack changes the current branch.
	// Restore the current branch (or its base) after the operation.
	//
	// TODO: Make an 'upstack restack' spice.Service method
	// that won't leave us on the wrong branch.
	var checkoutTarget string
	if currentBranch, err := repo.CurrentBranch(ctx); err != nil {
		if !errors.Is(err, git.ErrDetachedHead) {
			return fmt.Errorf("get current branch: %w", err)
		}

		// We still want to check out the original branch
		// if we're in detached HEAD state.
		head, err := repo.PeelToCommit(ctx, "HEAD")
		if err != nil {
			return fmt.Errorf("peel to commit: %w", err)
		}

		checkoutTarget = head.String()
	} else {
		if cmd.Name == currentBranch {
			checkoutTarget = base
		} else {
			checkoutTarget = currentBranch
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
				contCmd := []string{"branch", "delete"}
				if cmd.Force {
					contCmd = append(contCmd, "--force")
				}
				contCmd = append(contCmd, cmd.Name)
				return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
					Err:     err,
					Command: contCmd,
					Branch:  checkoutTarget,
					Message: fmt.Sprintf("interrupted: %v: branch deleted", cmd.Name),
				})
			}
		}
	}

	if err := repo.Checkout(ctx, checkoutTarget); err != nil {
		return fmt.Errorf("checkout %v: %w", checkoutTarget, err)
	}

	// If the branch exists, and is not reachable from HEAD,
	// git will refuse to delete it.
	// If we can prompt, ask the user to upgrade to a forceful deletion.
	if exists && !cmd.Force && opts.Prompt && !repo.IsAncestor(ctx, head, "HEAD") {
		log.Warnf("%v (%v) is not reachable from HEAD", cmd.Name, head.Short())
		prompt := ui.NewConfirm().
			WithTitlef("Delete %v anyway?", cmd.Name).
			WithDescriptionf("%v has not been merged into HEAD. This may result in data loss.", cmd.Name).
			WithValue(&cmd.Force)
		if err := ui.Run(prompt); err != nil {
			return fmt.Errorf("run prompt: %w", err)
		}
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

	if tracked {
		if err := svc.ForgetBranch(ctx, cmd.Name); err != nil {
			return fmt.Errorf("forget branch %v: %w", cmd.Name, err)
		}
	}

	return nil
}

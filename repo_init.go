package main

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/state"
)

type repoInitCmd struct {
	Trunk  string `help:"The name of the trunk branch"`
	Remote string `help:"The name of the remote to use for the trunk branch"`

	Reset bool `help:"Reset the store if it's already initialized"`
}

func (cmd *repoInitCmd) Run(ctx context.Context, log *log.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	if cmd.Remote == "" {
		remotes, err := repo.ListRemotes(ctx)
		if err != nil {
			return fmt.Errorf("list remotes: %w", err)
		}

		switch len(remotes) {
		case 0:
			// No remotes, we'll leave it empty.
			log.Warn("No remotes found. Commands that require a remote will fail.")
		case 1:
			log.Infof("Using remote: %s", remotes[0])
			cmd.Remote = remotes[0]
		default:
			opts := make([]huh.Option[string], len(remotes))
			for i, remote := range remotes {
				opts[i] = huh.NewOption(remote, remote)
			}

			prompt := huh.NewSelect[string]().
				Title("Pick a remote").
				Options(opts...).
				Value(&cmd.Remote)
			if err := prompt.Run(); err != nil {
				return fmt.Errorf("prompt for remote: %w", err)
			}
		}
	}

	if cmd.Trunk == "" {
		defaultTrunk, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("determine current branch: %w", err)
		}

		// If there's a remote, and it has a default branch,
		// use that as the default trunk branch in the prompt.
		if cmd.Remote != "" {
			if upstream, err := repo.DefaultBranch(ctx, cmd.Remote); err == nil {
				defaultTrunk = upstream
			}
		}

		localBranches, err := repo.LocalBranches(ctx)
		if err != nil {
			return fmt.Errorf("list local branches: %w", err)
		}
		sort.Strings(localBranches)

		switch len(localBranches) {
		case 0:
			// There are no branches with any commits,
			// but HEAD still points to a branch.
			// This will be true for new repositories
			// without any commits only.
			cmd.Trunk = defaultTrunk
		case 1:
			cmd.Trunk = localBranches[0]
		default:
			opts := make([]huh.Option[string], len(localBranches))
			for i, branch := range localBranches {
				opt := huh.NewOption(branch, branch)
				if branch == defaultTrunk {
					opt = opt.Selected(true)
				}
				opts[i] = opt
			}

			prompt := huh.NewSelect[string]().
				Title("Pick a trunk branch").
				Options(opts...).
				Value(&cmd.Trunk)
			if err := prompt.Run(); err != nil {
				return fmt.Errorf("prompt for branch: %w", err)
			}
		}
	}
	must.NotBeBlankf(cmd.Trunk, "trunk branch must have been set")

	_, err = state.InitStore(ctx, state.InitStoreRequest{
		Repository: repo,
		Trunk:      cmd.Trunk,
		Remote:     cmd.Remote,
		Reset:      cmd.Reset,
	})
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}

	log.Info("Initialized repository", "trunk", cmd.Trunk)
	return nil
}

// ensureStore will open the gs data store in the provided Git repository,
// initializing it with `gs repo init` if it hasn't already been initialized.
//
// This allows nearly any other command to work without initialization
// by auto-initializing the repository at that time.
func ensureStore(
	ctx context.Context,
	repo state.GitRepository,
	log *log.Logger,
) (*state.Store, error) {
	store, err := state.OpenStore(ctx, repo, log)
	if err == nil {
		return store, nil
	}

	if errors.Is(err, state.ErrUninitialized) {
		log.Info("Repository not initialized for use with gs. Initializing.")
		if err := (&repoInitCmd{}).Run(ctx, log); err != nil {
			return nil, fmt.Errorf("auto-initialize: %w", err)
		}

		// Assume initialization was a success.
		return state.OpenStore(ctx, repo, log)
	}

	return nil, fmt.Errorf("open store: %w", err)
}

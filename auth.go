package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
)

type authCmd struct {
	Login  authLoginCmd  `cmd:"" help:"Log in to a service"`
	Status authStatusCmd `cmd:"" help:"Show current login status"`
	Logout authLogoutCmd `cmd:"" help:"Log out of a service"`
}

// resolveForge resolves a forge by name.
// If name is unset, it will attempt to guess the forge based on the current
// repository's remote URL.
// If the forge cannot be guessed, it will prompt the user to select one
// if we're in interactive mode.
func resolveForge(ctx context.Context, log *log.Logger, globals *globalOptions, forgeID string) (forge.Forge, error) {
	if forgeID != "" {
		f, ok := forge.Lookup(forgeID)
		if !ok {
			log.Errorf("Forge ID must be one of: %s", forge.IDs())
			return nil, fmt.Errorf("unknown forge: %s", forgeID)
		}
		return f, nil
	}

	f, err := guessCurrentForge(ctx, log)
	if err == nil {
		return f, nil
	}

	if !globals.Prompt {
		log.Error("No Forge specified, and could not guess one from the repository", "error", err)
		return nil, fmt.Errorf("%w: please set a Forge explicitly", errNoPrompt)
	}

	var opts []ui.SelectOption[forge.Forge]
	forge.All(func(f forge.Forge) bool {
		opts = append(opts, ui.SelectOption[forge.Forge]{
			Label: f.ID(),
			Value: f,
		})
		return true
	})
	slices.SortFunc(opts, func(a, b ui.SelectOption[forge.Forge]) int {
		return cmp.Compare(a.Label, b.Label)
	})

	field := ui.NewSelect[forge.Forge]().
		WithTitle("Select a forge to log into").
		WithOptions(opts...).
		WithValue(&f)
	err = ui.Run(field)
	return f, err
}

// guessCurrentForge attempts to guess the current forge based on the
// current directory.
func guessCurrentForge(ctx context.Context, log *log.Logger) (forge.Forge, error) {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return nil, errors.New("not in a Git repository")
	}

	db := newRepoStorage(repo, log)
	store, err := state.OpenStore(ctx, db, log)
	if err != nil {
		return nil, errors.New("repository not initialized")
	}

	remote, err := store.Remote()
	if err != nil {
		return nil, errors.New("no remote set for repository")
	}

	remoteURL, err := repo.RemoteURL(ctx, remote)
	if err != nil {
		return nil, fmt.Errorf("get remote URL: %w", err)
	}

	forge, ok := forge.MatchForgeURL(remoteURL)
	if !ok {
		return nil, fmt.Errorf("no forge found for %s", remoteURL)
	}

	return forge, nil
}

package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
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

	Forge string `help:"Name of the forge to log into" placeholder:"NAME" predictor:"forges"`
}

// AfterApply makes the Forge available to all subcommands.
func (c *authCmd) AfterApply(
	ctx context.Context,
	kctx *kong.Context,
	log *log.Logger,
	globals *globalOptions,
) error {
	f, err := resolveForge(ctx, log, globals, c.Forge)
	if err != nil {
		return err
	}

	kctx.BindTo(f, (*forge.Forge)(nil))
	return nil
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
			var available []string
			for f := range forge.All {
				available = append(available, f.ID())
			}
			slices.Sort(available)

			log.Errorf("Forge ID must be one of: %s", strings.Join(available, ", "))
			return nil, fmt.Errorf("unknown forge: %s", forgeID)
		}
		return f, nil
	}

	f, err := guessCurrentForge(ctx, log)
	if err == nil {
		return f, nil
	}

	var opts []ui.SelectOption[forge.Forge]
	for f := range forge.All {
		opts = append(opts, ui.SelectOption[forge.Forge]{
			Label: f.ID(),
			Value: f,
		})
	}
	slices.SortFunc(opts, func(a, b ui.SelectOption[forge.Forge]) int {
		return cmp.Compare(a.Label, b.Label)
	})

	// If there's only one known Forge, there's no need to prompt.
	if len(opts) == 1 {
		return opts[0].Value, nil
	}

	if !globals.Prompt {
		log.Error("No Forge specified, and could not guess one from the repository", "error", err)
		return nil, fmt.Errorf("%w: please use the --forge flag", errNoPrompt)
	}

	field := ui.NewSelect[forge.Forge]().
		WithTitle("Select a Forge").
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

	// If the repository is already initialized with gs,
	// and a remote is configured, use the forge for that remote.
	var remote string
	if store, err := state.OpenStore(ctx, newRepoStorage(repo, log), log); err == nil {
		remote, err = store.Remote()
		if err != nil {
			remote = ""
		}
	}

	// Otherwise, look at the existing remotes.
	if remote == "" {
		remotes, err := repo.ListRemotes(ctx)
		if err != nil {
			return nil, fmt.Errorf("list remotes: %w", err)
		}
		switch len(remotes) {
		case 0:
			return nil, errors.New("no remote set for repository")

		case 1:
			remote = remotes[0]

		default:
			// Repository not initialized with gs
			// and has multiple remotes.
			// We can't guess the forge in this case.
			return nil, errors.New("multiple remotes found: initialize with gs first")
		}
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

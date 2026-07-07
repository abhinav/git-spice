package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/giturl"
	"go.abhg.dev/gs/internal/silog"
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
	log *silog.Logger,
	forges *forge.Registry,
	cmd *mainCmd,
	view ui.View,
) error {
	f, err := resolveForge(ctx, forges, log, view, c.Forge, cmd.Forge.Kind)
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
func resolveForge(
	ctx context.Context,
	forges *forge.Registry,
	log *silog.Logger,
	view ui.View,
	forgeID string,
	configuredKind string,
) (forge.Forge, error) {
	if wantForge := cmp.Or(forgeID, configuredKind); wantForge != "" {
		d, ok := forges.Lookup(wantForge)
		if !ok {
			var available []string
			for d := range forges.All() {
				available = append(available, d.ID())
			}
			slices.Sort(available)

			log.Errorf("Forge ID must be one of: %s", strings.Join(available, ", "))
			return nil, fmt.Errorf("unknown forge: %q", wantForge)
		}

		remoteURL, _, err := currentRemoteURL(ctx, log)
		if err != nil {
			remoteURL, err = giturl.Parse(d.BaseURL())
			if err != nil {
				return nil, fmt.Errorf("parse forge base URL: %w", err)
			}
		}

		f, err := d.New(remoteURL)
		if err != nil {
			return nil, fmt.Errorf("construct forge %q: %w", wantForge, err)
		}
		return f, nil
	}

	remoteURL, rawRemoteURL, err := currentRemoteURL(ctx, log)
	if err != nil {
		return nil, err
	}

	f, _, ok := forge.InferFromRemoteURL(forges, remoteURL)
	if ok {
		return f, nil
	}

	var opts []ui.SelectOption[forge.Definition]
	for d := range forges.All() {
		opts = append(opts, ui.SelectOption[forge.Definition]{
			Label: forge.GetDisplayName(d),
			Value: d,
		})
	}
	slices.SortFunc(opts, func(a, b ui.SelectOption[forge.Definition]) int {
		return cmp.Compare(a.Label, b.Label)
	})

	// If there's only one known Forge, there's no need to prompt.
	if len(opts) == 1 {
		return opts[0].Value.New(remoteURL)
	}

	if !ui.Interactive(view) {
		err := fmt.Errorf("no forge found for %s", rawRemoteURL)
		log.Error("No Forge specified, and could not guess one from the repository", "error", err)
		return nil, fmt.Errorf("%w: please use the --forge flag", errNoPrompt)
	}

	var selected forge.Definition
	field := ui.NewSelect[forge.Definition]().
		WithTitle("Select a Forge").
		WithOptions(opts...).
		WithValue(&selected)
	err = ui.Run(view, field)
	if err != nil {
		return nil, err
	}
	return selected.New(remoteURL)
}

func currentRemoteURL(ctx context.Context, log *silog.Logger) (*giturl.URL, string, error) {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return nil, "", errors.New("not in a Git repository")
	}

	// If the repository is already initialized with git-spice,
	// and a remote is configured, use the forge for that remote.
	var remote string
	if store, err := state.OpenStore(ctx, newRepoStorage(repo, log), log); err == nil {
		if r, err := store.Remote(); err == nil {
			remote = r.Upstream
		}
	}

	// Otherwise, look at the existing remotes.
	if remote == "" {
		remotes, err := repo.ListRemotes(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("list remotes: %w", err)
		}
		switch len(remotes) {
		case 0:
			return nil, "", errors.New("no remote set for repository")

		case 1:
			remote = remotes[0]

		default:
			// Repository not initialized with git-spice
			// and has multiple remotes.
			// We can't guess the forge in this case.
			return nil, "", fmt.Errorf("multiple remotes found: initialize with %s first", cli.Name())
		}
	}

	remoteURL, err := repo.RemoteURL(ctx, remote)
	if err != nil {
		return nil, "", fmt.Errorf("get remote URL: %w", err)
	}

	parsedRemoteURL, err := giturl.Parse(remoteURL)
	if err != nil {
		return nil, "", fmt.Errorf("parse remote URL: %w", err)
	}

	return parsedRemoteURL, remoteURL, nil
}

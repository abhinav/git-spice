package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/giturl"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/silog"
)

type unsupportedForgeError struct {
	Remote    string // required
	RemoteURL string // required
}

func (e *unsupportedForgeError) Error() string {
	return fmt.Sprintf("unsupported Git remote %q: %s", e.Remote, e.RemoteURL)
}

type notLoggedInError struct {
	Forge forge.Forge // required
}

func (e *notLoggedInError) Error() string {
	return "not logged in to " + e.Forge.ID()
}

// remoteURLer reads configured and transport-resolved remote URLs.
type remoteURLer interface {
	// RemoteConfigURL accepts a Git remote name and returns the configured
	// `remote.<name>.url` value before applying `url.*.insteadOf` rewriting.
	RemoteConfigURL(context.Context, string) (string, error)

	// RemoteURL accepts a Git remote name and returns the URL Git uses for
	// transport after applying `url.*.insteadOf` rewriting.
	RemoteURL(context.Context, string) (string, error)
}

var _ remoteURLer = (*git.Repository)(nil)

// remoteResolver resolves git-spice remotes to forge repository identities.
type remoteResolver struct {
	// Forges is the registry of forge implementations that may own a remote.
	Forges *forge.Registry // required

	// Repository is the Git repository whose remotes should be resolved.
	Repository remoteURLer // required

	// ForgeKind names the forge selected by configuration.
	//
	// If unset, remote URLs must identify their forge by host.
	ForgeKind string // required
}

// Resolve identifies the forge and repository for a configured Git remote.
func (r *remoteResolver) Resolve(
	ctx context.Context,
	remote string,
) (forge.Forge, forge.RepositoryID, error) {
	remoteURL, err := r.Repository.RemoteConfigURL(ctx, remote)
	if err != nil {
		return nil, nil, fmt.Errorf("get remote URL: %w", err)
	}

	parsedRemoteURL, err := giturl.Parse(remoteURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse remote URL: %w", err)
	}

	if r.ForgeKind != "" {
		resolvedRemoteURL, err := r.Repository.RemoteURL(ctx, remote)
		if err != nil {
			return nil, nil, fmt.Errorf("get resolved remote URL: %w", err)
		}

		parsedResolvedRemoteURL, err := giturl.Parse(resolvedRemoteURL)
		if err != nil {
			return nil, nil, fmt.Errorf("parse resolved remote URL: %w", err)
		}

		f, err := r.Forges.New(r.ForgeKind, parsedResolvedRemoteURL)
		if err != nil {
			if errors.Is(err, forge.ErrUnknown) {
				var available []string
				for d := range r.Forges.All() {
					available = append(available, d.ID())
				}
				slices.Sort(available)

				return nil, nil, fmt.Errorf(
					"unknown forge kind %q: expected one of: %s",
					r.ForgeKind,
					strings.Join(available, ", "),
				)
			}
			return nil, nil, fmt.Errorf("construct forge %q: %w", r.ForgeKind, err)
		}

		repoID, err := f.ParseRepositoryPath(parsedRemoteURL.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("parse remote path as %s: %w", f.ID(), err)
		}
		return f, repoID, nil
	}

	f, repoID, ok := forge.InferFromRemoteURL(r.Forges, parsedRemoteURL)
	if !ok {
		return nil, nil, &unsupportedForgeError{
			Remote:    remote,
			RemoteURL: remoteURL,
		}
	}
	return f, repoID, nil
}

// ResolveID identifies the repository for a configured Git remote.
func (r *remoteResolver) ResolveID(
	ctx context.Context,
	remote string,
) (forge.RepositoryID, error) {
	_, repoID, err := r.Resolve(ctx, remote)
	return repoID, err
}

// Open opens the forge repository associated with a configured Git remote.
func (r *remoteResolver) Open(
	ctx context.Context,
	stash secret.Stash,
	remote string,
) (forge.Repository, error) {
	f, repoID, err := r.Resolve(ctx, remote)
	if err != nil {
		return nil, err
	}

	return openForgeRepository(ctx, stash, f, repoID)
}

func openForgeRepository(
	ctx context.Context,
	stash secret.Stash,
	f forge.Forge,
	repoID forge.RepositoryID,
) (forge.Repository, error) {
	tok, err := f.LoadAuthenticationToken(stash)
	if err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			return nil, &notLoggedInError{Forge: f}
		}
		return nil, fmt.Errorf("load authentication token: %w", err)
	}

	return f.OpenRepository(ctx, tok, repoID)
}

func resolveRemoteRepository(
	ctx context.Context,
	log *silog.Logger,
	resolver *remoteResolver,
	remote string,
) (forge.Forge, forge.RepositoryID, error) {
	f, repoID, err := resolver.Resolve(ctx, remote)

	if unsupportedErr, ok := errors.AsType[*unsupportedForgeError](err); ok {
		log.Error("Could not guess repository from remote URL", "url", unsupportedErr.RemoteURL)
		log.Error("Are you sure the remote identifies a supported Git host?")
		log.Error("If this remote uses a Git host alias, run `git config spice.forge.kind <forge>`.")
	}

	return f, repoID, err
}

func openRepository(
	ctx context.Context,
	log *silog.Logger,
	stash secret.Stash,
	f forge.Forge,
	repo forge.RepositoryID,
) (forge.Repository, error) {
	forgeRepo, err := openForgeRepository(ctx, stash, f, repo)

	if notLoggedInErr, ok := errors.AsType[*notLoggedInError](err); ok {
		f := notLoggedInErr.Forge
		log.Errorf("No authentication token found for %s.", f.ID())
		log.Errorf("Try running `%s auth login --forge=%s`", cli.Name(), f.ID())
	}

	return forgeRepo, err
}

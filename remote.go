package main

import (
	"context"
	"errors"
	"fmt"

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

// remoteURLer reads Git's resolved URL for a named remote.
type remoteURLer interface {
	// RemoteURL accepts a Git remote name and returns the URL that Git uses
	// after applying its remote URL rewriting rules.
	//
	// It is equivalent to `git remote get-url <name>`.
	RemoteURL(context.Context, string) (string, error)
}

var _ remoteURLer = (*git.Repository)(nil)

// remoteResolver resolves git-spice remotes to forge repository identities.
type remoteResolver struct {
	// Forges is the registry of forge implementations that may own a remote.
	Forges *forge.Registry

	// Repository is the Git repository whose remotes should be resolved.
	Repository remoteURLer
}

// Resolve identifies the forge and repository for a configured Git remote.
func (r *remoteResolver) Resolve(
	ctx context.Context,
	remote string,
) (forge.Forge, forge.RepositoryID, error) {
	remoteURL, err := r.Repository.RemoteURL(ctx, remote)
	if err != nil {
		return nil, nil, fmt.Errorf("get remote URL: %w", err)
	}

	parsedRemoteURL, err := giturl.Parse(remoteURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse remote URL: %w", err)
	}

	f, repoID, ok := forge.FromRemoteURL(r.Forges, parsedRemoteURL)
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

// Attempts to open the forge.Repository associated with the given Git remote.
//
// Does not print any error messages to the user.
// Instead, returns one of the following errors:
//
//   - unsupportedForgeError if the remote URL does not match
//     any any known forges.
//   - notLoggedInError if the user is not authenticated with the forge.
func openRemoteRepositorySilent(
	ctx context.Context,
	stash secret.Stash,
	forges *forge.Registry,
	gitRepo *git.Repository,
	remote string,
) (forge.Repository, error) {
	f, repoID, err := (&remoteResolver{
		Forges:     forges,
		Repository: gitRepo,
	}).Resolve(ctx, remote)
	if err != nil {
		return nil, err
	}

	return openForgeRepository(ctx, stash, f, repoID)
}

func resolveRemoteRepositoryID(
	ctx context.Context,
	forges *forge.Registry,
	gitRepo *git.Repository,
	remote string,
) (forge.RepositoryID, error) {
	return (&remoteResolver{
		Forges:     forges,
		Repository: gitRepo,
	}).ResolveID(ctx, remote)
}

func resolveRemoteRepositorySilent(
	ctx context.Context,
	forges *forge.Registry,
	gitRepo *git.Repository,
	remote string,
) (forge.Forge, forge.RepositoryID, error) {
	return (&remoteResolver{
		Forges:     forges,
		Repository: gitRepo,
	}).Resolve(ctx, remote)
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
	forges *forge.Registry,
	gitRepo *git.Repository,
	remote string,
) (forge.Forge, forge.RepositoryID, error) {
	f, repoID, err := resolveRemoteRepositorySilent(ctx, forges, gitRepo, remote)

	if unsupportedErr, ok := errors.AsType[*unsupportedForgeError](err); ok {
		log.Error("Could not guess repository from remote URL", "url", unsupportedErr.RemoteURL)
		log.Error("Are you sure the remote identifies a supported Git host?")
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

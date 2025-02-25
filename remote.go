package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/secret"
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
	gitRepo *git.Repository,
	remote string,
) (forge.Repository, error) {
	remoteURL, err := gitRepo.RemoteURL(ctx, remote)
	if err != nil {
		return nil, fmt.Errorf("get remote URL: %w", err)
	}

	f, ok := forge.MatchForgeURL(forge.DefaultRegistry, remoteURL)
	if !ok {
		return nil, &unsupportedForgeError{
			Remote:    remote,
			RemoteURL: remoteURL,
		}
	}

	tok, err := f.LoadAuthenticationToken(stash)
	if err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			return nil, &notLoggedInError{Forge: f}
		}
		return nil, fmt.Errorf("load authentication token: %w", err)
	}

	return f.OpenURL(ctx, tok, remoteURL)
}

func openRemoteRepository(
	ctx context.Context,
	log *log.Logger,
	stash secret.Stash,
	gitRepo *git.Repository,
	remote string,
) (forge.Repository, error) {
	forgeRepo, err := openRemoteRepositorySilent(ctx, stash, gitRepo, remote)

	var (
		unsupportedErr *unsupportedForgeError
		notLoggedInErr *notLoggedInError
	)
	switch {
	case errors.As(err, &unsupportedErr):
		log.Error("Could not guess repository from remote URL", "url", unsupportedErr.RemoteURL)
		log.Error("Are you sure the remote identifies a supported Git host?")
		return nil, err

	case errors.As(err, &notLoggedInErr):
		f := notLoggedInErr.Forge
		log.Errorf("No authentication token found for %s.", f.ID())
		log.Errorf("Try running `gs auth login --forge=%s`", f.ID())
		return nil, err

	default:
		return forgeRepo, err
	}
}

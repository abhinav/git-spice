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

func openRemoteRepository(
	ctx context.Context,
	log *log.Logger,
	stash secret.Stash,
	gitRepo *git.Repository,
	remote string,
) (forge.Repository, error) {
	remoteURL, err := gitRepo.RemoteURL(ctx, remote)
	if err != nil {
		return nil, fmt.Errorf("get remote URL: %w", err)
	}

	f, ok := forge.MatchForgeURL(remoteURL)
	if !ok {
		log.Error("Could not guess repository from remote URL", "url", remoteURL)
		log.Error("Are you sure the remote identifies a supported Git host?")
		return nil, errors.New("unsupported Git remote URL")
	}

	tok, err := f.LoadAuthenticationToken(stash)
	if err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			log.Errorf("No authentication token found for %s.", f.ID())
			log.Errorf("Try running `gs auth login %s`", f.ID())
			return nil, errors.New("not logged in")
		}
		return nil, fmt.Errorf("load authentication token: %w", err)
	}

	return f.OpenURL(ctx, tok, remoteURL)
}

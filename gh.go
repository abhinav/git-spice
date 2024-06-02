package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
)

func openRemoteRepository(
	ctx context.Context,
	log *log.Logger,
	gitRepo *git.Repository,
	remote string,
) (forge.Repository, error) {
	remoteURL, err := gitRepo.RemoteURL(ctx, remote)
	if err != nil {
		return nil, fmt.Errorf("get remote URL: %w", err)
	}

	forgeRepo, err := forge.OpenRepositoryURL(ctx, remoteURL)
	if err != nil {
		if errors.Is(err, forge.ErrUnsupportedURL) {
			log.Error("Could not guess repository from remote URL", "url", remoteURL)
			log.Error("Are you sure the remote identifies a supported Git host?")
			return nil, err
		}
		return nil, fmt.Errorf("open repository URL: %w", err)
	}

	return forgeRepo, nil
}

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge/github"
	"go.abhg.dev/gs/internal/git"
)

func ensureGitHubForge(
	ctx context.Context,
	log *log.Logger,
	builder *github.Builder,
	repo *git.Repository,
	remote string,
) (*github.Forge, error) {
	remoteURL, err := repo.RemoteURL(ctx, remote)
	if err != nil {
		return nil, fmt.Errorf("get remote URL: %w", err)
	}

	forge, err := builder.New(ctx, remoteURL)
	if err != nil {
		if errors.Is(err, github.ErrUnsupportedURL) {
			log.Error("Could not guess GitHub repository from remote URL", "url", remoteURL)
			log.Error("Are you sure the remote is a GitHub repository?")
			return nil, err
		}
		return nil, fmt.Errorf("build GitHub Forge: %w", err)
	}

	return forge, nil
}

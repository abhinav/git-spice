package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/google/go-github/v61/github"
	"go.abhg.dev/gs/internal/gh"
	"go.abhg.dev/gs/internal/git"
	"golang.org/x/oauth2"
)

func ensureGitHubRepo(
	ctx context.Context,
	log *log.Logger,
	repo *git.Repository,
	remote string,
) (gh.RepoInfo, error) {
	remoteURL, err := repo.RemoteURL(ctx, remote)
	if err != nil {
		return gh.RepoInfo{}, fmt.Errorf("get remote URL: %w", err)
	}

	// TODO: Take GITHUB_GIT_URL into account for ParseRepoInfo.
	ghrepo, err := gh.ParseRepoInfo(remoteURL)
	if err != nil {
		log.Error("Could not guess GitHub repository from remote URL", "url", remoteURL)
		log.Error("Are you sure the remote is a GitHub repository?")
		return gh.RepoInfo{}, err
	}

	return ghrepo, nil
}

// TODO: move this into gh package
func newGitHubClient(ctx context.Context, tokenSource oauth2.TokenSource, opts *globalOptions) (*github.Client, error) {
	gh := github.NewClient(oauth2.NewClient(ctx, tokenSource))
	if opts.GithubAPIURL != "" {
		var err error
		gh, err = gh.WithEnterpriseURLs(opts.GithubAPIURL, gh.UploadURL.String())
		if err != nil {
			return nil, fmt.Errorf("set GitHub API URL: %w", err)
		}
	}

	return gh, nil
}

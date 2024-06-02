package github

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// Repository is a GitHub repository.
type Repository struct {
	owner, repo string
	repoID      githubv4.ID
	log         *log.Logger
	client      *githubv4.Client
}

var _ forge.Repository = (*Repository)(nil)

func newRepository(
	ctx context.Context,
	owner, repo string,
	log *log.Logger,
	client *githubv4.Client,
) (*Repository, error) {
	var q struct {
		Repository struct {
			ID githubv4.ID `graphql:"id"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}
	if err := client.Query(ctx, &q, map[string]any{
		"owner": githubv4.String(owner),
		"repo":  githubv4.String(repo),
	}); err != nil {
		return nil, fmt.Errorf("get repository ID: %w", err)
	}

	return &Repository{
		owner:  owner,
		repo:   repo,
		log:    log,
		client: client,
		repoID: q.Repository.ID,
	}, nil
}

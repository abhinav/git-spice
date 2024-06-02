// Package github defines a GitHub Forge.
package github

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/shurcooL/githubv4"
)

// Forge provides access to GitHub's API,
// while complying with the Forge interface.
type Forge struct {
	owner, repo string
	repoID      githubv4.ID
	log         *log.Logger
	client      *githubv4.Client
}

func newForge(
	ctx context.Context,
	owner, repo string,
	log *log.Logger,
	client *githubv4.Client,
) (*Forge, error) {
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

	return &Forge{
		owner:  owner,
		repo:   repo,
		log:    log,
		client: client,
		repoID: q.Repository.ID,
	}, nil
}

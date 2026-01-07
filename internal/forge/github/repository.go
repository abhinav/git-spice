package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
)

// Repository is a GitHub repository.
type Repository struct {
	owner, repo string
	repoID      githubv4.ID
	log         *silog.Logger
	client      *githubv4.Client
	forge       *Forge
}

var _ forge.Repository = (*Repository)(nil)

func newRepository(
	ctx context.Context,
	forge *Forge,
	owner, repo string,
	log *silog.Logger,
	client *githubv4.Client,
	repoID githubv4.ID,
) (*Repository, error) {
	log = log.With("repo", fmt.Sprintf("%s/%s", owner, repo))
	if repoID == "" || repoID == nil {
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

		repoID = q.Repository.ID
	}

	return &Repository{
		owner:  owner,
		repo:   repo,
		log:    log,
		client: client,
		repoID: repoID,
		forge:  forge,
	}, nil
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }

// userID looks up a user's GraphQL ID by login.
func (r *Repository) userID(ctx context.Context, login string) (githubv4.ID, error) {
	var query struct {
		User struct {
			ID githubv4.ID `graphql:"id"`
		} `graphql:"user(login: $login)"`
	}

	variables := map[string]any{
		"login": githubv4.String(login),
	}

	if err := r.client.Query(ctx, &query, variables); err != nil {
		return "", fmt.Errorf("query user: %w", err)
	}

	id := query.User.ID
	if id == "" || id == nil {
		return "", fmt.Errorf("user not found: %q", login)
	}

	return id, nil
}

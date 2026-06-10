package github

import (
	"context"
	"fmt"
	"sync"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
	"golang.org/x/sync/singleflight"
)

// Repository is a GitHub repository.
type Repository struct {
	owner, repo string
	repoID      githubv4.ID
	log         *silog.Logger
	client      *githubv4.Client
	forge       *Forge

	userIDsMu sync.Mutex // guards userIDs
	// userIDs caches successful login lookups for this repository.
	//
	// Pull request metadata operations can resolve the same login
	// through reviewers, assignees, or follow-up edits in one command.
	userIDs map[string]githubv4.ID
	// userIDGroup coalesces concurrent misses for the same login.
	userIDGroup singleflight.Group
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
		var err error
		repoID, err = repositoryGQLID(ctx, client, owner, repo)
		if err != nil {
			return nil, fmt.Errorf("get repository ID: %w", err)
		}
	}

	return &Repository{
		owner:   owner,
		repo:    repo,
		log:     log,
		client:  client,
		repoID:  repoID,
		forge:   forge,
		userIDs: make(map[string]githubv4.ID),
	}, nil
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }

func repositoryGQLID(
	ctx context.Context,
	client *githubv4.Client,
	owner, repo string,
) (githubv4.ID, error) {
	var q struct {
		Repository struct {
			ID githubv4.ID `graphql:"id"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}
	if err := client.Query(ctx, &q, map[string]any{
		"owner": githubv4.String(owner),
		"repo":  githubv4.String(repo),
	}); err != nil {
		return "", fmt.Errorf("query repository: %w", err)
	}
	return q.Repository.ID, nil
}

// userID looks up a user's GraphQL ID by login.
func (r *Repository) userID(ctx context.Context, login string) (githubv4.ID, error) {
	r.userIDsMu.Lock()
	id, ok := r.userIDs[login]
	r.userIDsMu.Unlock()
	if ok {
		// Another goroutine resolved this login
		// while the singleflight call was waiting for the lock.
		return id, nil
	}

	idAny, err, _ := r.userIDGroup.Do(login, func() (any, error) {
		r.userIDsMu.Lock()
		id, ok := r.userIDs[login]
		r.userIDsMu.Unlock()
		if ok {
			// Another goroutine resolved this login
			// while the singleflight call was waiting for the lock.
			return id, nil
		}

		id, err := r.queryUserID(ctx, login)
		if err != nil {
			return "", err
		}

		r.userIDsMu.Lock()
		if r.userIDs == nil {
			r.userIDs = make(map[string]githubv4.ID)
		}
		r.userIDs[login] = id
		r.userIDsMu.Unlock()

		return id, nil
	})
	if err != nil {
		return "", err
	}
	return idAny.(githubv4.ID), nil
}

func (r *Repository) queryUserID(ctx context.Context, login string) (githubv4.ID, error) {
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

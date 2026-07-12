package github

import (
	"context"
	"fmt"
	"sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/silog"
	"golang.org/x/sync/singleflight"
)

// Repository is a GitHub repository.
type Repository struct {
	owner, repo string
	repoID      github.ID
	log         *silog.Logger
	gateway     *github.Gateway
	forge       *Forge

	userIDsMu sync.Mutex // guards userIDs
	// userIDs caches successful login lookups for this repository.
	//
	// Pull request metadata operations can resolve the same login
	// through reviewers, assignees, or follow-up edits in one command.
	userIDs map[string]github.ID
	// userIDGroup coalesces concurrent misses for the same login.
	userIDGroup singleflight.Group
}

var _ forge.Repository = (*Repository)(nil)

func newRepository(
	ctx context.Context,
	forge *Forge,
	owner, repo string,
	log *silog.Logger,
	gateway *github.Gateway,
	repoID github.ID,
) (*Repository, error) {
	log = log.With("repo", fmt.Sprintf("%s/%s", owner, repo))
	if repoID == "" {
		var err error
		repositoryID, err := gateway.RepositoryID(ctx, owner, repo)
		if err != nil {
			return nil, fmt.Errorf("get repository ID: %w", err)
		}
		repoID = repositoryID
	}

	return &Repository{
		owner:   owner,
		repo:    repo,
		log:     log,
		gateway: gateway,
		repoID:  repoID,
		forge:   forge,
		userIDs: make(map[string]github.ID),
	}, nil
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }

// userID looks up a user's GraphQL ID by login.
func (r *Repository) userID(ctx context.Context, login string) (github.ID, error) {
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
			r.userIDs = make(map[string]github.ID)
		}
		r.userIDs[login] = id
		r.userIDsMu.Unlock()

		return id, nil
	})
	if err != nil {
		return "", err
	}
	return idAny.(github.ID), nil
}

func (r *Repository) queryUserID(ctx context.Context, login string) (github.ID, error) {
	id, err := r.gateway.UserID(ctx, login)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("user not found: %q", login)
	}

	return id, nil
}

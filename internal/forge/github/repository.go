package github

import (
	"context"
	"fmt"
	"net/url"
	"sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/silog"
)

// Repository is a GitHub repository.
type Repository struct {
	owner, repo string
	repoID      github.ID
	log         *silog.Logger
	gateway     *github.Gateway
	forge       *Forge

	identityIDsMu sync.RWMutex // guards userIDsCache and teamIDsCache
	// userIDsCache caches successful login lookups for this repository.
	//
	// Pull request metadata operations can resolve the same login
	// through reviewers, assignees, or follow-up edits in one command.
	userIDsCache map[string]github.ID

	// teamIDsCache caches successful organization and team slug lookups.
	teamIDsCache map[github.TeamName]github.ID
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
		owner:        owner,
		repo:         repo,
		log:          log,
		gateway:      gateway,
		repoID:       repoID,
		forge:        forge,
		userIDsCache: make(map[string]github.ID),
		teamIDsCache: make(map[github.TeamName]github.ID),
	}, nil
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }

var _ forge.WithComparisonURL = (*Repository)(nil)

// ComparisonURL returns a URL for a comparison on GitHub.
// See GitHub's [comparing commits documentation].
//
// [comparing commits documentation]: https://docs.github.com/en/pull-requests/committing-changes-to-your-project/viewing-and-comparing-commits/comparing-commits
func (r *Repository) ComparisonURL(req forge.ComparisonRequest) string {
	head := req.HeadURLEncoded()
	if req.HeadRepository != nil {
		headRepo := mustRepositoryID(req.HeadRepository)
		if headRepo.owner != r.owner || headRepo.name != r.repo {
			// GitHub qualifies a cross-fork head with its owner.
			head = url.PathEscape(headRepo.owner) + ":" + head
		}
	}
	return fmt.Sprintf("%s/%s/%s/compare/%s...%s",
		r.forge.URL(), r.owner, r.repo, req.BaseURLEncoded(), head)
}

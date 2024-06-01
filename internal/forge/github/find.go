package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v61/github"
	"go.abhg.dev/gs/internal/git"
)

// FindChangeItem is a single result from searching for changes in the
// repository.
type FindChangeItem struct {
	// ID is a unique identifier for the change.
	ID ChangeID

	// URL is the web URL at which the change can be viewed.
	URL string

	// Subject is the title of the change.
	Subject string

	// HeadHash is the hash of the commit at the top of the change.
	HeadHash git.Hash

	// BaseName is the name of the base branch
	// that this change is proposed against.
	BaseName string

	// Draft is true if the change is not yet ready to be reviewed.
	Draft bool
}

// TODO: Reduce filtering options in favor of explicit queries,
// e.g. "FindChangesForBranch" or "ListOpenChanges", etc.

// FindChangesByBranch searches for open changes with the given branch name.
// Returns [ErrNotFound] if no changes are found.
func (f *Forge) FindChangesByBranch(ctx context.Context, branch string) ([]FindChangeItem, error) {
	pulls, _, err := f.client.PullRequests.List(ctx, f.owner, f.repo, &github.PullRequestListOptions{
		State: "open",
		Head:  f.owner + ":" + branch,
	})
	if err != nil {
		return nil, fmt.Errorf("list pull requests: %w", err)
	}

	changes := make([]FindChangeItem, len(pulls))
	for i, pull := range pulls {
		changes[i] = FindChangeItem{
			ID:       ChangeID(pull.GetNumber()),
			URL:      pull.GetHTMLURL(),
			Subject:  pull.GetTitle(),
			BaseName: pull.GetBase().GetRef(),
			HeadHash: git.Hash(pull.GetHead().GetSHA()),
			Draft:    pull.GetDraft(),
		}
	}

	return changes, nil
}

// FindChangeByID searches for a change with the given ID.
func (f *Forge) FindChangeByID(ctx context.Context, id ChangeID) (*FindChangeItem, error) {
	pull, _, err := f.client.PullRequests.Get(ctx, f.owner, f.repo, int(id))
	if err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}

	return &FindChangeItem{
		ID:       ChangeID(pull.GetNumber()),
		URL:      pull.GetHTMLURL(),
		Subject:  pull.GetTitle(),
		BaseName: pull.GetBase().GetRef(),
		HeadHash: git.Hash(pull.GetHead().GetSHA()),
		Draft:    pull.GetDraft(),
	}, nil
}

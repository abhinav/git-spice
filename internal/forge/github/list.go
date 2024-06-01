package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v61/github"
	"go.abhg.dev/gs/internal/git"
)

// ListChangesOptions specifies options for listing changes in a repository.
type ListChangesOptions struct {
	// State specifies the state of changes to list.
	// This can be "open", "closed", or "all".
	// Defaults to "open".
	State string

	// Branch is the upstream branch to list changes for.
	// If unset, changes for all branches are listed.
	Branch string
}

// ListChangesItem is a single item in a list of changes
// returned by ListChanges.
type ListChangesItem struct {
	// ID is a unique identifier for the change.
	ID ChangeID
	// TODO: Type for ChangeID with String() that does "#num".

	// Note: This is always numeric for GitHub,
	// but this API is trying to generalize across forges.

	// URL is the web URL at which the change can be viewed.
	URL string

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

// ListChanges lists changes in a repository, optionally filtered by options.
func (f *Forge) ListChanges(ctx context.Context, opts ListChangesOptions) ([]ListChangesItem, error) {
	// TODO: pagination/iterator?
	pulls, _, err := f.client.PullRequests.List(ctx, f.owner, f.repo, &github.PullRequestListOptions{
		State: opts.State,
		Head:  f.owner + ":" + opts.Branch,
	})
	if err != nil {
		return nil, fmt.Errorf("list pull requests: %w", err)
	}

	items := make([]ListChangesItem, len(pulls))
	for i, pull := range pulls {
		items[i] = ListChangesItem{
			ID:       ChangeID(pull.GetNumber()),
			URL:      pull.GetHTMLURL(),
			BaseName: pull.GetBase().GetRef(),
			HeadHash: git.Hash(pull.GetHead().GetSHA()),
			Draft:    pull.GetDraft(),
		}
	}

	return items, nil
}

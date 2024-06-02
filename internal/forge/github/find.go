package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
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

type findPRNode struct {
	ID          githubv4.ID          `graphql:"id"`
	Number      githubv4.Int         `graphql:"number"`
	URL         githubv4.URI         `graphql:"url"`
	Title       githubv4.String      `graphql:"title"`
	HeadRefOid  githubv4.GitObjectID `graphql:"headRefOid"`
	BaseRefName githubv4.String      `graphql:"baseRefName"`
	IsDraft     githubv4.Boolean     `graphql:"isDraft"`
}

func (n *findPRNode) toFindChangeItem() FindChangeItem {
	return FindChangeItem{
		ID:       ChangeID(n.Number),
		URL:      n.URL.String(),
		Subject:  string(n.Title),
		BaseName: string(n.BaseRefName),
		HeadHash: git.Hash(n.HeadRefOid),
		Draft:    bool(n.IsDraft),
	}
}

// FindChangesByBranch searches for open changes with the given branch name.
// Returns [ErrNotFound] if no changes are found.
func (f *Forge) FindChangesByBranch(ctx context.Context, branch string) ([]FindChangeItem, error) {
	var q struct {
		Repository struct {
			PullRequests struct {
				Nodes []findPRNode `graphql:"nodes"`
			} `graphql:"pullRequests(first: 10, states: OPEN, headRefName: $branch)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	if err := f.client.Query(ctx, &q, map[string]any{
		"owner":  githubv4.String(f.owner),
		"repo":   githubv4.String(f.repo),
		"branch": githubv4.String(branch),
	}); err != nil {
		return nil, fmt.Errorf("find changes by branch: %w", err)
	}

	changes := make([]FindChangeItem, len(q.Repository.PullRequests.Nodes))
	for i, node := range q.Repository.PullRequests.Nodes {
		changes[i] = node.toFindChangeItem()
	}

	return changes, nil
}

// FindChangeByID searches for a change with the given ID.
func (f *Forge) FindChangeByID(ctx context.Context, id ChangeID) (*FindChangeItem, error) {
	var q struct {
		Repository struct {
			PullRequest findPRNode `graphql:"pullRequest(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	if err := f.client.Query(ctx, &q, map[string]any{
		"owner":  githubv4.String(f.owner),
		"repo":   githubv4.String(f.repo),
		"number": githubv4.Int(id),
	}); err != nil {
		return nil, fmt.Errorf("find change by ID: %w", err)
	}

	item := q.Repository.PullRequest.toFindChangeItem()
	return &item, nil
}

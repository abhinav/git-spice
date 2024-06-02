package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
)

type findPRNode struct {
	ID          githubv4.ID          `graphql:"id"`
	Number      githubv4.Int         `graphql:"number"`
	URL         githubv4.URI         `graphql:"url"`
	Title       githubv4.String      `graphql:"title"`
	HeadRefOid  githubv4.GitObjectID `graphql:"headRefOid"`
	BaseRefName githubv4.String      `graphql:"baseRefName"`
	IsDraft     githubv4.Boolean     `graphql:"isDraft"`
}

func (n *findPRNode) toFindChangeItem() *forge.FindChangeItem {
	return &forge.FindChangeItem{
		ID:       forge.ChangeID(n.Number),
		URL:      n.URL.String(),
		Subject:  string(n.Title),
		BaseName: string(n.BaseRefName),
		HeadHash: git.Hash(n.HeadRefOid),
		Draft:    bool(n.IsDraft),
	}
}

// FindChangesByBranch searches for open changes with the given branch name.
// Returns [ErrNotFound] if no changes are found.
func (r *Repository) FindChangesByBranch(ctx context.Context, branch string) ([]*forge.FindChangeItem, error) {
	var q struct {
		Repository struct {
			PullRequests struct {
				Nodes []findPRNode `graphql:"nodes"`
			} `graphql:"pullRequests(first: 10, states: OPEN, headRefName: $branch)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	if err := r.client.Query(ctx, &q, map[string]any{
		"owner":  githubv4.String(r.owner),
		"repo":   githubv4.String(r.repo),
		"branch": githubv4.String(branch),
	}); err != nil {
		return nil, fmt.Errorf("find changes by branch: %w", err)
	}

	changes := make([]*forge.FindChangeItem, len(q.Repository.PullRequests.Nodes))
	for i, node := range q.Repository.PullRequests.Nodes {
		changes[i] = node.toFindChangeItem()
	}

	return changes, nil
}

// FindChangeByID searches for a change with the given ID.
func (r *Repository) FindChangeByID(ctx context.Context, id forge.ChangeID) (*forge.FindChangeItem, error) {
	var q struct {
		Repository struct {
			PullRequest findPRNode `graphql:"pullRequest(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	if err := r.client.Query(ctx, &q, map[string]any{
		"owner":  githubv4.String(r.owner),
		"repo":   githubv4.String(r.repo),
		"number": githubv4.Int(id),
	}); err != nil {
		return nil, fmt.Errorf("find change by ID: %w", err)
	}

	return q.Repository.PullRequest.toFindChangeItem(), nil
}

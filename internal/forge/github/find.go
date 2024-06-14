package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
)

type findPRNode struct {
	ID          githubv4.ID               `graphql:"id"`
	Number      githubv4.Int              `graphql:"number"`
	URL         githubv4.URI              `graphql:"url"`
	Title       githubv4.String           `graphql:"title"`
	State       githubv4.PullRequestState `graphql:"state"`
	HeadRefOid  githubv4.GitObjectID      `graphql:"headRefOid"`
	BaseRefName githubv4.String           `graphql:"baseRefName"`
	IsDraft     githubv4.Boolean          `graphql:"isDraft"`
}

func (n *findPRNode) toFindChangeItem() *forge.FindChangeItem {
	return &forge.FindChangeItem{
		ID: &PR{
			Number: int(n.Number),
			GQLID:  n.ID,
		},
		URL:      n.URL.String(),
		State:    forgeChangeState(n.State),
		Subject:  string(n.Title),
		BaseName: string(n.BaseRefName),
		HeadHash: git.Hash(n.HeadRefOid),
		Draft:    bool(n.IsDraft),
	}
}

func pullRequestState(s forge.ChangeState) githubv4.PullRequestState {
	switch s {
	case forge.ChangeOpen:
		return githubv4.PullRequestStateOpen
	case forge.ChangeClosed:
		return githubv4.PullRequestStateClosed
	case forge.ChangeMerged:
		return githubv4.PullRequestStateMerged
	default:
		return ""
	}
}

func forgeChangeState(s githubv4.PullRequestState) forge.ChangeState {
	switch s {
	case githubv4.PullRequestStateOpen:
		return forge.ChangeOpen
	case githubv4.PullRequestStateClosed:
		return forge.ChangeClosed
	case githubv4.PullRequestStateMerged:
		return forge.ChangeMerged
	default:
		return 0
	}
}

// FindChangesByBranch searches for changes with the given branch name.
// It returns both, open and closed changes.
// Only recent changes are returned, limited by the given limit.
func (r *Repository) FindChangesByBranch(ctx context.Context, branch string, opts forge.FindChangesOptions) ([]*forge.FindChangeItem, error) {
	if opts.Limit == 0 {
		opts.Limit = 10
	}

	var q struct {
		Repository struct {
			PullRequests struct {
				Nodes []findPRNode `graphql:"nodes"`
			} `graphql:"pullRequests(first: $limit, headRefName: $branch, states: $states, orderBy: {field: UPDATED_AT, direction: DESC})"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	vars := map[string]any{
		"owner":  githubv4.String(r.owner),
		"repo":   githubv4.String(r.repo),
		"branch": githubv4.String(branch),
		"limit":  githubv4.Int(opts.Limit),
	}
	if opts.State == 0 {
		vars["states"] = []githubv4.PullRequestState{
			githubv4.PullRequestStateOpen,
			githubv4.PullRequestStateClosed,
			githubv4.PullRequestStateMerged,
		}
	} else {
		vars["states"] = []githubv4.PullRequestState{pullRequestState(opts.State)}
	}

	if err := r.client.Query(ctx, &q, vars); err != nil {
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
		"number": githubv4.Int(mustPR(id).Number),
	}); err != nil {
		return nil, fmt.Errorf("find change by ID: %w", err)
	}

	return q.Repository.PullRequest.toFindChangeItem(), nil
}

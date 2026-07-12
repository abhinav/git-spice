package github

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/git"
)

func toFindChangeItem(n *github.PullRequest) *forge.FindChangeItem {
	var labels []string
	if len(n.Labels.Nodes) > 0 {
		labels = make([]string, len(n.Labels.Nodes))
		for i, node := range n.Labels.Nodes {
			labels[i] = node.Name
		}
	}

	var reviewers []string
	if len(n.ReviewRequests.Nodes) > 0 {
		reviewers = make([]string, len(n.ReviewRequests.Nodes))
		for i, node := range n.ReviewRequests.Nodes {
			reviewers[i] = node.RequestedReviewer.Login
		}
	}

	var assignees []string
	if len(n.Assignees.Nodes) > 0 {
		assignees = make([]string, len(n.Assignees.Nodes))
		for i, node := range n.Assignees.Nodes {
			assignees[i] = node.Login
		}
	}

	return &forge.FindChangeItem{
		ID: &PR{
			Number: n.Number,
			GQLID:  n.ID,
		},
		URL:       n.URL,
		State:     forgeChangeState(n.State),
		Subject:   n.Title,
		BaseName:  n.BaseRefName,
		HeadHash:  git.Hash(n.HeadRefOID),
		Draft:     n.IsDraft,
		Labels:    labels,
		Reviewers: reviewers,
		Assignees: assignees,
	}
}

func pullRequestState(s forge.ChangeState) github.PullRequestState {
	switch s {
	case forge.ChangeOpen:
		return github.PullRequestStateOpen
	case forge.ChangeClosed:
		return github.PullRequestStateClosed
	case forge.ChangeMerged:
		return github.PullRequestStateMerged
	default:
		return 0
	}
}

func forgeChangeState(s github.PullRequestState) forge.ChangeState {
	switch s {
	case github.PullRequestStateOpen:
		return forge.ChangeOpen
	case github.PullRequestStateClosed:
		return forge.ChangeClosed
	case github.PullRequestStateMerged:
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
	pushRepository := opts.PushRepository
	if pushRepository == nil {
		pushRepository = r.repositoryID()
	}

	var states []github.PullRequestState
	if opts.State == 0 {
		states = []github.PullRequestState{
			github.PullRequestStateOpen,
			github.PullRequestStateClosed,
			github.PullRequestStateMerged,
		}
	} else {
		states = []github.PullRequestState{pullRequestState(opts.State)}
	}

	nodes, err := r.gateway.FindPullRequests(ctx, r.owner, r.repo, branch, opts.Limit, states)
	if err != nil {
		return nil, fmt.Errorf("find changes by branch: %w", err)
	}

	changes := make([]*forge.FindChangeItem, 0, len(nodes))
	for _, node := range nodes {
		nodeRepository := RepositoryID{
			url:   r.forge.URL(),
			owner: node.HeadRepository.Owner.Login,
			name:  node.HeadRepository.Name,
		}
		if nodeRepository.String() != pushRepository.String() {
			continue
		}
		changes = append(changes, toFindChangeItem(node))
	}

	return changes, nil
}

// FindChangeByID searches for a change with the given ID.
func (r *Repository) FindChangeByID(ctx context.Context, id forge.ChangeID) (*forge.FindChangeItem, error) {
	pr := mustPR(id)
	node, err := r.gateway.PullRequest(ctx, r.owner, r.repo, pr.Number)
	if err != nil {
		return nil, fmt.Errorf("find change by ID: %w", err)
	}

	return toFindChangeItem(node), nil
}

func (r *Repository) repositoryID() *RepositoryID {
	return &RepositoryID{
		url:   r.forge.URL(),
		owner: r.owner,
		name:  r.repo,
	}
}

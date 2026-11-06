package azuredevops

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
	"go.abhg.dev/gs/internal/git"
)

// FindChangesByBranch searches for pull requests with the given branch
// as the head branch.
func (r *Repository) FindChangesByBranch(
	ctx context.Context,
	branch string,
	opts forge.FindChangesOptions,
) ([]*forge.FindChangeItem, error) {
	sourceRef := "refs/heads/" + branch

	// Map state filter.
	var status azuredevops.PullRequestStatus
	switch opts.State {
	case forge.ChangeOpen:
		status = azuredevops.PullRequestStatusActive
	case forge.ChangeMerged:
		status = azuredevops.PullRequestStatusCompleted
	case forge.ChangeClosed:
		status = azuredevops.PullRequestStatusAbandoned
	default:
		// All states.
		status = azuredevops.PullRequestStatusUnknown
	}

	limit := opts.Limit
	if limit == 0 {
		limit = 10
	}

	var sourceRepositoryID string
	if opts.PushRepository != nil {
		pushRepository, err := r.getRepository(ctx, opts.PushRepository)
		if err != nil {
			return nil, fmt.Errorf("resolve push repository: %w", err)
		}
		if pushRepository.ID == "" {
			return nil, errors.New("push repository has no ID")
		}
		sourceRepositoryID = pushRepository.ID
	}

	prs, err := r.gateway.FindPullRequests(ctx, &azuredevops.FindPullRequestsInput{
		Project:          r.project(),
		Repository:       r.repositoryID(),
		SourceRef:        sourceRef,
		SourceRepository: sourceRepositoryID,
		Status:           status,
		Limit:            limit,
	})
	if err != nil {
		return nil, fmt.Errorf("get pull requests: %w", err)
	}

	items := make([]*forge.FindChangeItem, 0, len(prs))
	for _, pr := range prs {
		item, err := r.prToFindChangeItem(ctx, pr)
		if err != nil {
			r.log.Warn("Failed to convert PR", "err", err)
			continue
		}
		items = append(items, item)
	}

	return items, nil
}

// FindChangeByID looks up a pull request by its ID.
func (r *Repository) FindChangeByID(
	ctx context.Context,
	id forge.ChangeID,
) (*forge.FindChangeItem, error) {
	prID := mustPR(id).Number

	pr, err := r.gateway.PullRequest(ctx, r.project(), r.repositoryID(), prID)
	if err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}

	return r.prToFindChangeItem(ctx, pr)
}

func (r *Repository) prToFindChangeItem(
	ctx context.Context,
	pr *azuredevops.PullRequest,
) (*forge.FindChangeItem, error) {
	if pr == nil {
		return nil, errors.New("nil pull request")
	}

	prID := pr.ID

	state := mapPRStatusToChangeState(pr.Status)

	headHash := git.Hash(pr.HeadCommit)

	baseName := trimRefPrefix(pr.TargetRef)

	labels, err := r.prLabels(ctx, prID, pr.Labels)
	if err != nil {
		return nil, fmt.Errorf("get labels: %w", err)
	}

	reviewers, err := r.prReviewers(ctx, prID, pr.Reviewers)
	if err != nil {
		return nil, fmt.Errorf("get reviewers: %w", err)
	}

	return &forge.FindChangeItem{
		ID:        &PR{Number: prID},
		URL:       r.repoID.ChangeURL(&PR{Number: prID}),
		State:     state,
		Subject:   pr.Title,
		HeadHash:  headHash,
		BaseName:  baseName,
		Draft:     pr.Draft,
		Labels:    labels,
		Reviewers: reviewers,
	}, nil
}

func trimRefPrefix(ref string) string {
	const prefix = "refs/heads/"
	if len(ref) > len(prefix) && ref[:len(prefix)] == prefix {
		return ref[len(prefix):]
	}
	return ref
}

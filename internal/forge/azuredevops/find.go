package azuredevops

import (
	"context"
	"fmt"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.abhg.dev/gs/internal/forge"
	internalgit "go.abhg.dev/gs/internal/git"
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
	var status *git.PullRequestStatus
	switch opts.State {
	case forge.ChangeOpen:
		status = statusPtr(git.PullRequestStatusValues.Active)
	case forge.ChangeMerged:
		status = statusPtr(git.PullRequestStatusValues.Completed)
	case forge.ChangeClosed:
		status = statusPtr(git.PullRequestStatusValues.Abandoned)
	default:
		// All states.
		status = statusPtr(git.PullRequestStatusValues.All)
	}

	limit := opts.Limit
	if limit == 0 {
		limit = 10
	}

	prs, err := r.client.gitClient.GetPullRequests(ctx, git.GetPullRequestsArgs{
		Project:      strPtr(r.project()),
		RepositoryId: strPtr(r.repositoryID()),
		SearchCriteria: &git.GitPullRequestSearchCriteria{
			SourceRefName: &sourceRef,
			Status:        status,
		},
		Top: &limit,
	})
	if err != nil {
		return nil, fmt.Errorf("get pull requests: %w", err)
	}

	if prs == nil {
		return nil, nil
	}

	items := make([]*forge.FindChangeItem, 0, len(*prs))
	for _, pr := range *prs {
		item, err := r.prToFindChangeItem(&pr)
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

	pr, err := r.client.gitClient.GetPullRequest(ctx, git.GetPullRequestArgs{
		Project:       strPtr(r.project()),
		RepositoryId:  strPtr(r.repositoryID()),
		PullRequestId: &prID,
	})
	if err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}

	return r.prToFindChangeItem(pr)
}

func (r *Repository) prToFindChangeItem(
	pr *git.GitPullRequest,
) (*forge.FindChangeItem, error) {
	if pr == nil {
		return nil, fmt.Errorf("nil pull request")
	}

	prID := 0
	if pr.PullRequestId != nil {
		prID = *pr.PullRequestId
	}

	state := forge.ChangeOpen
	if pr.Status != nil {
		state = mapPRStatusToChangeState(*pr.Status)
	}

	var headHash internalgit.Hash
	if pr.LastMergeSourceCommit != nil && pr.LastMergeSourceCommit.CommitId != nil {
		headHash = internalgit.Hash(*pr.LastMergeSourceCommit.CommitId)
	}

	baseName := ""
	if pr.TargetRefName != nil {
		baseName = trimRefPrefix(*pr.TargetRefName)
	}

	subject := ""
	if pr.Title != nil {
		subject = *pr.Title
	}

	isDraft := false
	if pr.IsDraft != nil {
		isDraft = *pr.IsDraft
	}

	return &forge.FindChangeItem{
		ID:       &PR{Number: prID},
		URL:      r.repoID.ChangeURL(&PR{Number: prID}),
		State:    state,
		Subject:  subject,
		HeadHash: headHash,
		BaseName: baseName,
		Draft:    isDraft,
		// Labels, Reviewers, Assignees deferred to post-MVP.
	}, nil
}

func trimRefPrefix(ref string) string {
	const prefix = "refs/heads/"
	if len(ref) > len(prefix) && ref[:len(prefix)] == prefix {
		return ref[len(prefix):]
	}
	return ref
}

func statusPtr(s git.PullRequestStatus) *git.PullRequestStatus {
	return &s
}

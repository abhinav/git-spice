package azuredevops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
)

const _unsupportedAssigneesWarning = "Azure DevOps does not support PR assignees; " +
	"ignoring --assign flags"

// SubmitChange creates a new pull request in the repository.
func (r *Repository) SubmitChange(
	ctx context.Context,
	req forge.SubmitChangeRequest,
) (forge.SubmitChangeResult, error) {
	r.warnUnsupportedSubmitAssignees(req)

	// Azure DevOps requires full ref names.
	sourceRef := "refs/heads/" + req.Head
	targetRef := "refs/heads/" + req.Base

	createInput := azuredevops.CreatePullRequestInput{
		Project:     r.project(),
		Repository:  r.repositoryID(),
		Title:       req.Subject,
		Description: req.Body,
		SourceRef:   sourceRef,
		TargetRef:   targetRef,
		Draft:       req.Draft,
	}
	if req.PushRepository != nil {
		pushRepository, err := r.getRepository(ctx, req.PushRepository)
		if err != nil {
			return forge.SubmitChangeResult{}, fmt.Errorf(
				"resolve push repository: %w", err,
			)
		}
		createInput.ForkSource = pushRepository
	}

	pr, err := r.gateway.CreatePullRequest(ctx, &createInput)
	if err != nil {
		if exists, existsErr := r.refExists(ctx, req.Base); existsErr == nil && !exists {
			return forge.SubmitChangeResult{}, fmt.Errorf(
				"create pull request: %w",
				errors.Join(forge.ErrUnsubmittedBase, err),
			)
		}
		return forge.SubmitChangeResult{}, fmt.Errorf("create pull request: %w", err)
	}

	prID := pr.ID

	r.log.Debug("Created pull request",
		"pr", prID,
		"url", r.repoID.ChangeURL(&PR{Number: prID}),
	)

	if err := r.addLabelsToPullRequest(ctx, prID, req.Labels); err != nil {
		return forge.SubmitChangeResult{}, fmt.Errorf("add labels to pull request: %w", err)
	}

	if err := r.addReviewersToPullRequest(ctx, prID, req.Reviewers); err != nil {
		return forge.SubmitChangeResult{}, fmt.Errorf("add reviewers to pull request: %w", err)
	}

	return forge.SubmitChangeResult{
		ID:  &PR{Number: prID},
		URL: r.repoID.ChangeURL(&PR{Number: prID}),
	}, nil
}

func (r *Repository) warnUnsupportedSubmitAssignees(req forge.SubmitChangeRequest) {
	if len(req.Assignees) > 0 {
		r.log.Warn(_unsupportedAssigneesWarning)
	}
}

func (r *Repository) refExists(ctx context.Context, branch string) (bool, error) {
	filter := "heads/" + strings.TrimPrefix(branch, "refs/heads/")
	exists, err := r.gateway.RefExists(
		ctx, r.project(), r.repositoryID(), filter,
	)
	if err != nil {
		return false, fmt.Errorf("get refs: %w", err)
	}
	return exists, nil
}

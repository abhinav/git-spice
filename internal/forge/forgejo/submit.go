package forgejo

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
)

// SubmitChange creates a new change in a repository.
func (r *Repository) SubmitChange(
	ctx context.Context,
	req forge.SubmitChangeRequest,
) (forge.SubmitChangeResult, error) {
	labels, err := r.labelIDs(ctx, req.Labels)
	if err != nil {
		return forge.SubmitChangeResult{}, fmt.Errorf("resolve labels: %w", err)
	}

	title := req.Subject
	if req.Draft {
		title = _draftPrefix + " " + title
	}

	input := &forgejo.CreatePullRequestOption{
		Title:     title,
		Body:      req.Body,
		Head:      req.Head,
		Base:      req.Base,
		Draft:     req.Draft,
		Labels:    labels,
		Assignees: req.Assignees,
	}
	if req.PushRepository != nil {
		input.Head = mustRepositoryID(req.PushRepository).owner + ":" + req.Head
	}

	pr, _, err := r.client.PullRequestCreate(ctx, r.owner, r.repo, input)
	if err != nil {
		if errors.Is(err, forgejo.ErrNotFound) {
			return forge.SubmitChangeResult{},
				errors.Join(forge.ErrUnsubmittedBase, err)
		}
		return forge.SubmitChangeResult{}, fmt.Errorf("create pull request: %w", err)
	}

	if len(req.Reviewers) > 0 {
		_, err := r.client.PullReviewRequestCreate(
			ctx,
			r.owner,
			r.repo,
			pr.Index,
			&forgejo.PullReviewRequestOptions{Reviewers: req.Reviewers},
		)
		if err != nil {
			return forge.SubmitChangeResult{},
				fmt.Errorf("request reviewers: %w", err)
		}
	}

	r.log.Debug("Created pull request", "pr", pr.Index, "url", pr.HTMLURL)
	return forge.SubmitChangeResult{
		ID:  &PR{Number: pr.Index},
		URL: pr.HTMLURL,
	}, nil
}

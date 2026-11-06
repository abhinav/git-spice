package gitea

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

// _draftPrefix is the WIP prefix Gitea uses to mark draft pull requests.
// Gitea does not support the `draft` field in PR creation; it uses the title prefix.
const _draftPrefix = "WIP:"

// _draftRegex matches any of the WIP/draft title prefixes Gitea recognizes.
var _draftRegex = regexp.MustCompile(`(?i)^\s*(WIP:|\[WIP\]|Draft:|\[Draft\])\s*`)

// SubmitChange creates a new pull request in a repository.
func (r *Repository) SubmitChange(ctx context.Context, req forge.SubmitChangeRequest) (forge.SubmitChangeResult, error) {
	// For cross-repository (fork) PRs, Gitea requires "owner:branch" for the
	// head field.
	head := req.Head
	if req.PushRepository != nil {
		pushRID := mustRepositoryID(req.PushRepository)
		head = pushRID.owner + ":" + req.Head
	}

	// Gitea uses the "WIP:" title prefix to mark draft PRs,
	// not the `draft` API field (which is ignored in Gitea 1.22).
	title := req.Subject
	if req.Draft {
		title = _draftPrefix + " " + title
	}

	input := &giteagw.CreatePullRequestOption{
		Title: title,
		Head:  head,
		Base:  req.Base,
	}

	if req.Body != "" {
		input.Body = req.Body
	}

	if len(req.Labels) > 0 {
		labelIDs, err := r.ensureLabels(ctx, req.Labels)
		if err != nil {
			return forge.SubmitChangeResult{}, fmt.Errorf("ensure labels: %w", err)
		}
		input.Labels = labelIDs
	}

	// Note: reviewers are added via a dedicated endpoint after PR creation;
	// the Reviewers field in CreatePullRequestOption is ignored by Gitea 1.22.

	if len(req.Assignees) > 0 {
		input.Assignees = req.Assignees
	}

	pr, _, err := r.client.PullCreate(ctx, r.owner, r.repo, input)
	if err != nil {
		// A 404 from PR creation (after the repo was already verified to exist)
		// means the base branch does not exist on the remote.
		if errors.Is(err, giteagw.ErrNotFound) || isBaseNotFound(err) {
			return forge.SubmitChangeResult{}, fmt.Errorf("create pull request: %w", forge.ErrUnsubmittedBase)
		}
		return forge.SubmitChangeResult{}, fmt.Errorf("create pull request: %w", err)
	}
	r.log.Debug("Created pull request", "pr", pr.Number, "url", pr.HTMLURL)

	// Gitea ignores the Reviewers field in CreatePullRequestOption.
	// Add reviewer requests via the dedicated endpoint.
	if len(req.Reviewers) > 0 {
		if _, err := r.client.ReviewRequestCreate(ctx, r.owner, r.repo, pr.Number, req.Reviewers); err != nil {
			return forge.SubmitChangeResult{}, fmt.Errorf("add reviewers: %w", err)
		}
	}

	return forge.SubmitChangeResult{
		ID:  &PR{Number: pr.Number},
		URL: pr.HTMLURL,
	}, nil
}

// isBaseNotFound reports whether an API error indicates that the base branch
// does not exist on the remote.
//
// Gitea returns "BaseNotExist" as the message when the base branch is missing.
func isBaseNotFound(err error) bool {
	var apiErr *giteagw.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	msg := strings.ToLower(apiErr.Message)
	return msg == "basenotexist" ||
		strings.Contains(msg, "base branch does not exist") ||
		strings.Contains(msg, "target branch does not exist") ||
		strings.Contains(msg, "base branch is invalid") ||
		strings.Contains(msg, "invalid base branch")
}

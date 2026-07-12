package github

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
)

// SubmitChange creates a new change in a repository.
func (r *Repository) SubmitChange(ctx context.Context, req forge.SubmitChangeRequest) (forge.SubmitChangeResult, error) {
	input := github.CreatePullRequestInput{
		RepositoryID: r.repoID,
		Title:        req.Subject,
		BaseRefName:  req.Base,
		HeadRefName:  req.Head,
	}
	if req.PushRepository != nil {
		pushRepository := mustRepositoryID(req.PushRepository)
		if pushRepository.owner != r.owner || pushRepository.name != r.repo {
			pushRepositoryID, err := r.gateway.RepositoryID(
				ctx,
				pushRepository.owner,
				pushRepository.name,
			)
			if err != nil {
				return forge.SubmitChangeResult{}, fmt.Errorf("get push repository ID: %w", err)
			}

			// GitHub documents fork heads as owner:branch,
			// but GraphQL also exposes headRepositoryId to identify
			// the fork repository unambiguously. Send both for fork PRs:
			// the qualified head name is human-readable in diagnostics,
			// and the repository ID avoids ambiguity when repository names
			// or ownership relationships are unusual.
			input.HeadRefName = pushRepository.owner + ":" + req.Head
			input.HeadRepositoryID = &pushRepositoryID
		}
	}
	if req.Body != "" {
		input.Body = &req.Body
	}
	if req.Draft {
		draft := true
		input.Draft = &draft
	}

	pullRequest, err := r.gateway.CreatePullRequest(ctx, &input)
	if err != nil {
		// If the base branch has not been pushed yet,
		// the error is:
		//   {
		//      "type": "UNPROCESSABLE",
		//      "path": "createPullRequest",
		//      "message": "..., No commits between $base and $head, ..."
		//   }
		// String matching is not the best way to handle this,
		// so if the error is unprocessable,
		// we'll check if the repository has the base branch.
		if errors.Is(err, github.ErrUnprocessable) {
			if exists, existsErr := r.RefExists(ctx, "refs/heads/"+req.Base); existsErr == nil && !exists {
				return forge.SubmitChangeResult{}, errors.Join(forge.ErrUnsubmittedBase, err)
			}
		}

		return forge.SubmitChangeResult{}, fmt.Errorf("create pull request: %w", err)
	}

	r.log.Debug("Created pull request", "pr", pullRequest.Number, "url", pullRequest.URL)

	pullRequestID := pullRequest.ID
	if err := r.addPullRequestMetadata(ctx, pullRequestMetadataRequest{
		PullRequestID: pullRequestID,
		Labels:        req.Labels,
		Reviewers:     req.Reviewers,
		Assignees:     req.Assignees,
	}); err != nil {
		return forge.SubmitChangeResult{}, err
	}

	return forge.SubmitChangeResult{
		ID: &PR{
			Number: pullRequest.Number,
			GQLID:  pullRequestID,
		},
		URL: pullRequest.URL,
	}, nil
}

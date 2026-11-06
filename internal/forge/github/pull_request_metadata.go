package github

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"go.abhg.dev/gs/internal/gateway/github"
)

type pullRequestMetadataRequest struct {
	// PullRequestID identifies the pull request to update.
	PullRequestID github.ID

	// Labels contains label names to add.
	Labels []string

	// Reviewers contains user logins and organization/team names.
	Reviewers []string

	// Assignees contains user logins to assign.
	Assignees []string
}

func (r *Repository) addPullRequestMetadata(ctx context.Context, req pullRequestMetadataRequest) error {
	if len(req.Labels) == 0 && len(req.Reviewers) == 0 && len(req.Assignees) == 0 {
		return nil
	}

	reviewerUsers, reviewerTeams := reviewerNames(req.Reviewers)
	var (
		labelIDs []github.ID
		labelErr error

		identityUserIDs []github.ID
		identityTeamIDs []github.ID
		identityErr     error
	)
	identityUsers := make([]string, 0, len(reviewerUsers)+len(req.Assignees))
	identityUsers = append(identityUsers, reviewerUsers...)
	identityUsers = append(identityUsers, req.Assignees...)

	// Label and identity lookups are independent. Resolve them concurrently,
	// while combining reviewer and assignee users into one identity lookup so
	// shared and distinct users require only one gateway operation.
	var resolveGroup sync.WaitGroup
	if len(req.Labels) > 0 {
		resolveGroup.Go(func() {
			labelIDs, labelErr = r.ensureLabels(ctx, req.Labels)
		})
	}
	if len(identityUsers) > 0 || len(reviewerTeams) > 0 {
		resolveGroup.Go(func() {
			identityUserIDs, identityTeamIDs, identityErr = r.identityIDs(
				ctx, identityUsers, reviewerTeams,
			)
		})
	}
	resolveGroup.Wait()

	if labelErr != nil {
		return fmt.Errorf("get label IDs: %w", labelErr)
	}
	if identityErr != nil {
		return fmt.Errorf("resolve identities: %w", identityErr)
	}

	input := github.PullRequestMetadataInput{
		PullRequestID: req.PullRequestID,
		LabelIDs:      labelIDs,
	}
	if len(req.Reviewers) > 0 {
		input.ReviewerUserIDs = identityUserIDs[:len(reviewerUsers)]
		input.ReviewerTeamIDs = identityTeamIDs
	}
	if len(req.Assignees) > 0 {
		assigneeIDs := identityUserIDs[len(reviewerUsers):]
		slices.Sort(assigneeIDs)
		input.AssigneeIDs = slices.Compact(assigneeIDs)
	}

	if err := r.gateway.AddPullRequestMetadata(ctx, &input); err != nil {
		return fmt.Errorf("add pull request metadata: %w", err)
	}
	return nil
}

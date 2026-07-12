package github

import (
	"context"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/gateway/github"
)

func (r *Repository) addAssigneesToPullRequest(ctx context.Context, assignees []string, prGraphQLID github.ID) error {
	if len(assignees) == 0 {
		return nil
	}

	assigneeIDs, _, err := r.identityIDs(ctx, assignees, nil)
	if err != nil {
		return fmt.Errorf("get assignee IDs: %w", err)
	}
	slices.Sort(assigneeIDs)
	assigneeIDs = slices.Compact(assigneeIDs)

	if err := r.gateway.AddAssigneesToAssignable(
		ctx,
		prGraphQLID,
		assigneeIDs,
	); err != nil {
		return fmt.Errorf("add assignees to assignable: %w", err)
	}

	return nil
}

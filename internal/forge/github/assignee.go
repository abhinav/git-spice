package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
)

func (r *Repository) addAssigneesToPullRequest(ctx context.Context, assignees []string, prGraphQLID githubv4.ID) error {
	if len(assignees) == 0 {
		return nil
	}

	assigneeIDs, err := r.assigneeIDs(ctx, assignees)
	if err != nil {
		return fmt.Errorf("get assignee IDs: %w", err)
	}

	var m struct {
		AddAssigneesToAssignable struct {
			ClientMutationID githubv4.String `graphql:"clientMutationId"`
		} `graphql:"addAssigneesToAssignable(input: $input)"`
	}

	input := githubv4.AddAssigneesToAssignableInput{
		AssignableID: prGraphQLID,
		AssigneeIDs:  assigneeIDs,
	}

	if err := r.gh4.Mutate(ctx, &m, input, nil); err != nil {
		return fmt.Errorf("add assignees to assignable: %w", err)
	}

	return nil
}

// assigneeIDs resolves assignee logins to GitHub user IDs.
// The returned slice may be shorter than the input
// because duplicate logins are automatically deduplicated.
func (r *Repository) assigneeIDs(ctx context.Context, assignees []string) ([]githubv4.ID, error) {
	ids := make([]githubv4.ID, 0, len(assignees))
	seen := make(map[string]struct{}, len(assignees))
	for _, assignee := range assignees {
		if _, ok := seen[assignee]; ok {
			continue
		}
		seen[assignee] = struct{}{}

		id, err := r.assigneeID(ctx, assignee)
		if err != nil {
			return nil, fmt.Errorf("resolve assignee %q: %w", assignee, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *Repository) assigneeID(ctx context.Context, login string) (githubv4.ID, error) {
	var q struct {
		User struct {
			ID githubv4.ID `graphql:"id"`
		} `graphql:"user(login: $login)"`
	}
	vars := map[string]any{
		"login": githubv4.String(login),
	}
	if err := r.gh4.Query(ctx, &q, vars); err != nil {
		return "", fmt.Errorf("query user: %w", err)
	}
	if q.User.ID == nil || q.User.ID == "" {
		return "", fmt.Errorf("user %q not found", login)
	}

	return q.User.ID, nil
}

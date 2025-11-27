package gitlab

import (
	"context"
	"errors"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func (r *Repository) assigneeIDs(ctx context.Context, assignees []string) ([]int, error) {
	ids := make([]int, 0, len(assignees))
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

func (r *Repository) assigneeID(ctx context.Context, username string) (int, error) {
	if username == "" {
		return 0, errors.New("assignee username is empty")
	}

	users, _, err := r.client.Users.ListUsers(&gitlab.ListUsersOptions{
		Username: gitlab.Ptr(username),
	}, gitlab.WithContext(ctx))
	if err != nil {
		return 0, fmt.Errorf("list users: %w", err)
	}
	if len(users) == 0 {
		return 0, fmt.Errorf("user %q not found", username)
	}

	id := users[0].ID
	return id, nil
}

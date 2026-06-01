package forgejo

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/gateway/forgejo"
)

func labelNames(labels []*forgejo.Label) []string {
	if len(labels) == 0 {
		return nil
	}
	names := make([]string, len(labels))
	for i, label := range labels {
		names[i] = label.Name
	}
	return names
}

func userLogins(users []*forgejo.User) []string {
	if len(users) == 0 {
		return nil
	}
	logins := make([]string, len(users))
	for i, user := range users {
		logins[i] = user.Login
		if logins[i] == "" {
			logins[i] = user.UserName
		}
	}
	return logins
}

func (r *Repository) labelIDs(
	ctx context.Context,
	names []string,
) ([]int64, error) {
	if len(names) == 0 {
		return nil, nil
	}

	idByName := make(map[string]int64, len(names))
	opts := &forgejo.ListOptions{
		Page:  1,
		Limit: 50,
	}
	for {
		labels, response, err := r.client.LabelList(
			ctx, r.owner, r.repo, opts,
		)
		if err != nil {
			return nil, fmt.Errorf("list labels: %w", err)
		}

		for _, label := range labels {
			idByName[label.Name] = label.ID
		}
		if labelsResolved(idByName, names) || response.NextPage == 0 {
			break
		}
		opts.Page = int64(response.NextPage)
	}

	ids := make([]int64, 0, len(names))
	for _, name := range names {
		id, ok := idByName[name]
		if !ok {
			return nil, fmt.Errorf("label %q not found", name)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func labelsResolved(idByName map[string]int64, names []string) bool {
	for _, name := range names {
		if _, ok := idByName[name]; !ok {
			return false
		}
	}
	return true
}

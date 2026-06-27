package forgejo

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/gateway/forgejo"
)

var errLabelNotFound = errors.New("label not found")

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

	ids := make([]int64, len(names))
	for idx, name := range names {
		id, err := r.ensureLabel(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("ensure label %q: %w", name, err)
		}
		ids[idx] = id
	}
	return ids, nil
}

func (r *Repository) ensureLabel(ctx context.Context, name string) (int64, error) {
	id, err := r.labelID(ctx, name)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, errLabelNotFound) {
		return 0, err
	}

	label, _, err := r.client.LabelCreate(
		ctx,
		r.owner,
		r.repo,
		&forgejo.CreateLabelOption{
			Name:  name,
			Color: "ededed",
		},
	)
	if err != nil {
		return 0, fmt.Errorf("create label: %w", err)
	}
	return label.ID, nil
}

func (r *Repository) labelID(ctx context.Context, name string) (int64, error) {
	idByName := make(map[string]int64, 1)
	opts := &forgejo.ListOptions{
		Page:  1,
		Limit: 50,
	}
	for {
		labels, response, err := r.client.LabelList(
			ctx, r.owner, r.repo, opts,
		)
		if err != nil {
			return 0, fmt.Errorf("list labels: %w", err)
		}

		for _, label := range labels {
			idByName[label.Name] = label.ID
		}
		if _, ok := idByName[name]; ok || response.NextPage == 0 {
			break
		}
		opts.Page = int64(response.NextPage)
	}

	id, ok := idByName[name]
	if !ok {
		return 0, errLabelNotFound
	}
	return id, nil
}

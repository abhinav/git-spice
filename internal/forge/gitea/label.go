package gitea

import (
	"context"
	"fmt"

	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

const _defaultLabelColor = "#ededed"

// ensureLabels returns Gitea label IDs for names,
// creating missing repository labels before applying them.
func (r *Repository) ensureLabels(ctx context.Context, names []string) ([]int64, error) {
	if len(names) == 0 {
		return nil, nil
	}

	nameToID, err := r.labelIDs(ctx)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(names))
	for _, name := range names {
		id, ok := nameToID[name]
		if !ok {
			r.log.Infof("Label does not exist, creating: %v", name)
			label, _, err := r.client.LabelCreate(ctx, r.owner, r.repo, &giteagw.CreateLabelOption{
				Name:  name,
				Color: _defaultLabelColor,
			})
			if err != nil {
				return nil, fmt.Errorf("create label %q: %w", name, err)
			}
			id = label.ID
			nameToID[name] = id
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *Repository) labelIDs(ctx context.Context) (map[string]int64, error) {
	nameToID := make(map[string]int64)
	page := int64(1)
	for {
		labels, resp, err := r.client.LabelList(ctx, r.owner, r.repo, &giteagw.ListLabelsOptions{
			ListOptions: giteagw.ListOptions{Page: page, Limit: 50},
		})
		if err != nil {
			return nil, fmt.Errorf("list labels: %w", err)
		}
		for _, l := range labels {
			nameToID[l.Name] = l.ID
		}
		if resp.NextPage == 0 {
			break
		}
		page = int64(resp.NextPage)
	}
	return nameToID, nil
}

// currentLabelIDs fetches the current label IDs for a PR.
func (r *Repository) currentLabelIDs(ctx context.Context, prNumber int64) ([]int64, error) {
	pr, _, err := r.client.PullGet(ctx, r.owner, r.repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("get PR labels: %w", err)
	}
	ids := make([]int64, len(pr.Labels))
	for i, l := range pr.Labels {
		ids[i] = l.ID
	}
	return ids, nil
}

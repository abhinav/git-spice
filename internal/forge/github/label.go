package github

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.abhg.dev/gs/internal/gateway/github"
)

func (r *Repository) ensureLabels(ctx context.Context, labelNames []string) ([]github.ID, error) {
	// TODO:
	// cache label IDs in repo-level state to avoid querying every time.
	if len(labelNames) == 0 {
		return nil, nil
	}

	labelIDs, err := r.gateway.LabelIDs(ctx, r.owner, r.repo, labelNames)
	if err != nil {
		return nil, fmt.Errorf("query labels: %w", err)
	}

	resolved := make(map[string]github.ID, len(labelNames))
	var missing []string
	for i, labelName := range labelNames {
		if labelID := labelIDs[i]; labelID != "" {
			resolved[labelName] = labelID
			r.log.Debug("Resolved label ID", "name", labelName, "id", labelID)
			continue
		}
		if _, seen := resolved[labelName]; !seen {
			resolved[labelName] = ""
			missing = append(missing, labelName)
		}
	}

	createdIDs := make([]github.ID, len(missing))
	createErrs := make([]error, len(missing))
	var createGroup sync.WaitGroup
	for i, labelName := range missing {
		createGroup.Go(func() {
			r.log.Infof("Label does not exist, creating: %v", labelName)
			labelID, err := r.CreateLabel(ctx, labelName)
			if err != nil {
				createErrs[i] = fmt.Errorf("ensure label %q: %w", labelName, err)
				return
			}
			createdIDs[i] = labelID
			r.log.Debug("Resolved label ID", "name", labelName, "id", labelID)
		})
	}
	createGroup.Wait()
	if err := errors.Join(createErrs...); err != nil {
		return nil, err
	}
	for i, labelName := range missing {
		resolved[labelName] = createdIDs[i]
	}

	for i, labelName := range labelNames {
		labelIDs[i] = resolved[labelName]
	}
	return labelIDs, nil
}

// CreateLabel creates a label in the repository with the given name
// and returns its GraphQL ID.
func (r *Repository) CreateLabel(ctx context.Context, name string) (github.ID, error) {
	color := "EDEDED" // TODO: randomize this color
	id, err := r.gateway.CreateLabel(ctx, r.repoID, color, name)
	if err != nil {
		if errors.Is(err, github.ErrUnprocessable) {
			// GitHub returns Unprocessable if the label already exists.
			// If two concurrent requests try to create the same label,
			// and one of them wins, we can use the ID from the other request.
			r.log.Debug("Label might have been created by another request, querying", "name", name, "error", err)
			ids, queryErr := r.gateway.LabelIDs(ctx, r.owner, r.repo, []string{name})
			if queryErr == nil && ids[0] != "" {
				return ids[0], nil
			}
		}
		return "", fmt.Errorf("create label mutation: %w", err)
	}

	return id, nil
}

// DeleteLabel deletes a label from the repository by its ID.
// Use CreateLabel to get the ID of a new label.
// If the label does not exist, it returns nil error.
func (r *Repository) DeleteLabel(ctx context.Context, labelID string) error {
	if err := r.gateway.DeleteLabel(ctx, github.ID(labelID)); err != nil {
		if !errors.Is(err, github.ErrNotFound) {
			return fmt.Errorf("delete label mutation: %w", err)
		}
	}

	return nil
}

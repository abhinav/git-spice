package github

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"go.abhg.dev/gs/internal/gateway/github"
)

func (r *Repository) addLabelsToPullRequest(ctx context.Context, labels []string, prGraphQLID github.ID) error {
	if len(labels) == 0 {
		return nil
	}
	labelIDs, err := r.ensureLabels(ctx, labels)
	if err != nil {
		return fmt.Errorf("get label IDs: %w", err)
	}

	// NB:
	// addLabelsToLabelable ignores labels that are already present
	// on the pull request, so we don't need to check for that.
	if err := r.gateway.AddLabelsToLabelable(
		ctx,
		prGraphQLID,
		labelIDs,
	); err != nil {
		return fmt.Errorf("add labels to labelable: %w", err)
	}
	return nil
}

func (r *Repository) ensureLabels(ctx context.Context, labelNames []string) ([]github.ID, error) {
	// TODO:
	// cache label IDs in repo-level state to avoid querying every time.
	if len(labelNames) == 0 {
		return nil, nil
	}

	idxc := make(chan int)
	var (
		wg sync.WaitGroup

		mu   sync.Mutex // guards errs
		errs []error
	)
	labelIDs := make([]github.ID, len(labelNames)) // pre-allocate to fill without locking
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for idx := range idxc {
				labelName := labelNames[idx]

				labelID, err := r.ensureLabel(ctx, labelName)
				if err != nil {
					mu.Lock()
					errs = append(errs, fmt.Errorf("ensure label %q: %w", labelName, err))
					mu.Unlock()
					continue
				}

				r.log.Debug("Resolved label ID", "name", labelName, "id", labelID)
				labelIDs[idx] = labelID
			}
		})
	}

	for idx := range labelNames {
		idxc <- idx
	}
	close(idxc)
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	return labelIDs, nil
}

func (r *Repository) ensureLabel(ctx context.Context, labelName string) (github.ID, error) {
	labelID, err := r.LabelID(ctx, labelName)
	if err == nil {
		return labelID, nil
	}

	if !errors.Is(err, ErrLabelNotFound) {
		return "", fmt.Errorf("query label: %w", err)
	}

	r.log.Infof("Label does not exist, creating: %v", labelName)
	labelID, err = r.CreateLabel(ctx, labelName)
	if err != nil {
		return "", fmt.Errorf("create label: %w", err)
	}

	return labelID, nil
}

// ErrLabelNotFound indicates that a label that we were expecting
// was not found in the repository.
var ErrLabelNotFound = errors.New("label not found")

// LabelID returns the ID of a label by its name.
// It returns ErrLabelNotFound if the label does not exist.
func (r *Repository) LabelID(ctx context.Context, name string) (github.ID, error) {
	id, err := r.gateway.LabelID(ctx, r.owner, r.repo, name)
	if err != nil {
		return "", fmt.Errorf("query labels: %w", err)
	}
	if id == "" {
		return "", ErrLabelNotFound
	}
	return id, nil
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
			if id, err := r.LabelID(ctx, name); err == nil {
				return id, nil
			}
		}
		return "", fmt.Errorf("create label mutation: %w", err)
	}

	return id, nil
}

// DeleteLabel deletes a label from the repository by its ID.
// Use CreateLabel or LabelID to get the ID of a label.
// If the label does not exist, it returns nil error.
func (r *Repository) DeleteLabel(ctx context.Context, labelID string) error {
	if err := r.gateway.DeleteLabel(ctx, github.ID(labelID)); err != nil {
		if !errors.Is(err, github.ErrNotFound) {
			return fmt.Errorf("delete label mutation: %w", err)
		}
	}

	return nil
}

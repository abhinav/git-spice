package github

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/graphqlutil"
)

func (r *Repository) addLabelsToPullRequest(ctx context.Context, labels []string, prGraphQLID githubv4.ID) error {
	if len(labels) == 0 {
		return nil
	}
	labelIDs, err := r.ensureLabels(ctx, labels)
	if err != nil {
		return fmt.Errorf("get label IDs: %w", err)
	}

	var addLabelsM struct {
		AddLabelsToLabelable struct {
			Clientmutationid githubv4.String `graphql:"clientMutationId"`
		} `graphql:"addLabelsToLabelable(input: $input)"`
	}

	labelsInput := githubv4.AddLabelsToLabelableInput{
		LabelableID: prGraphQLID,
		LabelIDs:    labelIDs,
	}

	if err := r.client.Mutate(ctx, &addLabelsM, labelsInput, nil); err != nil {
		return fmt.Errorf("add labels to labelable: %w", err)
	}
	return nil
}

func (r *Repository) ensureLabels(ctx context.Context, labelNames []string) ([]githubv4.ID, error) {
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
	// TODO: Instead of a fan out search like this,
	// we can dynamically construct a GraphQL query like so:
	//
	//     repository(owner: $owner, name: $name) {
	//         _1: label(name: $label_1) { id }
	//         _2: label(name: $label_2) { id }
	//         ...
	//      }
	//
	// One query instead of many.
	labelIDs := make([]githubv4.ID, len(labelNames)) // pre-allocate to fill without locking
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
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
		}()
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

func (r *Repository) ensureLabel(ctx context.Context, labelName string) (githubv4.ID, error) {
	labelID, err := r.labelID(ctx, labelName)
	if err == nil {
		return labelID, nil
	}

	if !errors.Is(err, errLabelDoesNotExist) {
		return nil, fmt.Errorf("query label: %w", err)
	}

	r.log.Infof("Label does not exist, creating: %v", labelName)
	labelID, err = r.createLabel(ctx, labelName)
	if err != nil {
		return "", fmt.Errorf("create label: %w", err)
	}

	return labelID, nil
}

var errLabelDoesNotExist = errors.New("label not found")

func (r *Repository) labelID(ctx context.Context, name string) (githubv4.ID, error) {
	var query struct {
		Repository struct {
			Label struct {
				ID githubv4.ID `graphql:"id"`
			} `graphql:"label(name: $label)"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}

	variables := map[string]any{
		"owner": githubv4.String(r.owner),
		"name":  githubv4.String(r.repo),
		"label": githubv4.String(name),
	}
	if err := r.client.Query(ctx, &query, variables); err != nil {
		return "", fmt.Errorf("query labels: %w", err)
	}

	if query.Repository.Label.ID == "" {
		return "", errLabelDoesNotExist
	}

	return query.Repository.Label.ID, nil
}

func (r *Repository) createLabel(ctx context.Context, name string) (githubv4.ID, error) {
	var m struct {
		CreateLabel struct {
			Label struct {
				ID githubv4.ID `graphql:"id"`
			} `graphql:"label"`
		} `graphql:"createLabel(input: $input)"`
	}

	color := "EDEDED" // TODO: randomize this color
	input := githubv4.CreateLabelInput{
		RepositoryID: r.repoID,
		Name:         githubv4.String(name),
		Color:        githubv4.String(color),
	}

	if err := r.client.Mutate(ctx, &m, input, nil); err != nil {
		if errors.Is(err, graphqlutil.ErrUnprocessable) {
			// GitHub returns Unprocessable if the label already exists.
			// If two concurrent requests try to create the same label,
			// and one of them wins, we can use the ID from the other request.
			r.log.Debug("Label might have been created by another request, querying", "name", name, "error", err)
			if id, err := r.labelID(ctx, name); err == nil {
				return id, nil
			}
		}
		return "", fmt.Errorf("create label mutation: %w", err)
	}

	return m.CreateLabel.Label.ID, nil
}

package state

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/git"
)

// UpdateRequest is a request to add, update, or delete information about branches.
type UpdateRequest struct {
	// Upserts are requests to add or update information about branches.
	Upserts []UpsertRequest

	// Deletes are requests to delete information about branches.
	Deletes []string

	// Message is a message specifying the reason for the update.
	// This will be persisted in the Git commit message.
	Message string
}

// UpsertRequest is a request to add or update information about a branch.
type UpsertRequest struct {
	// Name is the name of the branch.
	Name string

	// Base branch to update to.
	//
	// Leave empty to keep the current base.
	Base string

	// BaseHash is the last known hash of the base branch.
	// This is used to detect if the base branch has been updated.
	//
	// Leave empty to keep the current base hash.
	BaseHash git.Hash

	// PR is the number of the pull request associated with the branch.
	// Zero if the branch is not associated with a PR yet.
	//
	// Leave nil to keep the current PR.
	PR *int
}

// Update upates the store with the parameters in the request.
func (s *Store) Update(ctx context.Context, req *UpdateRequest) error {
	if req.Message == "" {
		req.Message = fmt.Sprintf("update at %s", time.Now().Format(time.RFC3339))
	}

	sets := make([]setRequest, len(req.Upserts))
	for i, req := range req.Upserts {
		if req.Name == "" {
			return fmt.Errorf("upsert [%d]: branch name is required", i)
		}
		if req.Name == s.trunk {
			return fmt.Errorf("upsert [%d]: trunk branch is not managed by gs", i)
		}

		var b branchState
		if prev, err := s.Lookup(ctx, req.Name); err != nil {
			if !errors.Is(err, ErrNotExist) {
				return fmt.Errorf("get branch: %w", err)
			}
			// Branch does not exist yet.
			// Everything is already set to the zero value.
		} else {
			b.PR = prev.PR
			b.Base = branchStateBase{
				Name: prev.Base,
				Hash: prev.BaseHash.String(),
			}
		}

		if req.Base != "" {
			b.Base.Name = req.Base
		}
		if req.BaseHash != "" {
			b.Base.Hash = req.BaseHash.String()
		}
		if req.PR != nil {
			b.PR = *req.PR
		}

		if b.Base.Name == "" {
			return fmt.Errorf("branch %q (%d) would have no base", req.Name, i)
		}

		sets = append(sets, setRequest{
			Key: s.branchJSON(req.Name),
			Val: b,
		})
	}

	deletes := make([]string, len(req.Deletes))
	for i, name := range req.Deletes {
		deletes[i] = s.branchJSON(name)
	}

	err := s.b.Update(ctx, updateRequest{
		Sets: sets,
		Dels: deletes,
		Msg:  req.Message,
	})
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	return nil
}

// PR is a helper to create a pointer to an integer
// for the UpsertRequest PR field.
func PR(n int) *int {
	return &n
}

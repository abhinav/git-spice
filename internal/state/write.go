package state

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/git"
)

// SetRemote changes teh remote name configured for the repository.
func (s *Store) SetRemote(ctx context.Context, remote string) error {
	var info repoInfo
	if err := s.b.Get(ctx, _repoJSON, &info); err != nil {
		return fmt.Errorf("get repo info: %w", err)
	}
	info.Remote = remote

	if err := info.Validate(); err != nil {
		// Technically impossible if state was already validated
		// but worth checking to be sure.
		return fmt.Errorf("would corrupt state: %w", err)
	}

	err := s.b.Update(ctx, updateRequest{
		Sets: []setRequest{
			{
				Key: _repoJSON,
				Val: info,
			},
		},
		Msg: fmt.Sprintf("set remote: %v", remote),
	})
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	return nil
}

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
	// Leave zero to keep the current PR.
	PR int

	// UpstreamBranch is the name of the upstream branch to track.
	// Leave empty to stop tracking an upstream branch.
	UpstreamBranch string
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
			return fmt.Errorf("upsert [%d]: trunk branch is not managed by git-spice", i)
		}

		b, err := s.lookupBranchState(ctx, req.Name)
		if err != nil {
			if !errors.Is(err, ErrNotExist) {
				return fmt.Errorf("get branch: %w", err)
			}
			// Branch does not exist yet.
			b = &branchState{}
		}

		if req.Base != "" {
			b.Base.Name = req.Base
		}
		if req.BaseHash != "" {
			b.Base.Hash = req.BaseHash.String()
		}
		if req.PR != 0 {
			b.GitHub = &branchGitHubState{
				PR: req.PR,
			}
		}
		if req.UpstreamBranch != "" {
			b.Upstream = &branchUpstreamState{
				Branch: req.UpstreamBranch,
			}
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

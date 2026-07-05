package spice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
)

// LookupBranchResponse is the response to a LookupBranch request.
// It includes information about the tracked branch.
type LookupBranchResponse struct {
	// Base is the base branch configured
	// for the requested branch.
	Base string

	// BaseHash is the last known hash of the base branch.
	// This may not match the current hash of the base branch.
	BaseHash git.Hash

	// Change is information about the published change
	// associated with the branch.
	//
	// This is nil if the branch hasn't been published yet.
	Change forge.ChangeMetadata

	// UpstreamBranch is the name of the upstream branch
	// or an empty string if the branch is not tracking an upstream branch.
	UpstreamBranch string

	// Head is the commit at the head of the branch.
	Head git.Hash

	// MergedDownstack is a list of branches that were previously
	// downstack from this branch and have since been merged into trunk.
	//
	// This is used to correctly display the history of the branch.
	MergedDownstack []json.RawMessage
}

// DeletedBranchError is returned when a branch was deleted out of band.
//
// This error is used to indicate that the branch does not exist,
// but its base might.
type DeletedBranchError struct {
	Name string

	Base     string
	BaseHash git.Hash
}

func (e *DeletedBranchError) Error() string {
	return fmt.Sprintf("tracked branch %v was deleted out of band", e.Name)
}

// LookupBranch returns information about a branch tracked by git-spice.
//
// It returns [git.ErrNotExist] if the branch is not known to the repository,
// [state.ErrNotExist] if the branch is not tracked,
// or a [DeletedBranchError] if the branch is tracked, but was deleted out of band.
func (s *Service) LookupBranch(ctx context.Context, name string) (*LookupBranchResponse, error) {
	resp, storeErr := s.store.LookupBranch(ctx, name)
	head, gitErr := s.repo.PeelToCommit(ctx, name)

	// Handle all scenarios:
	//
	// storeErr | gitErr | Result
	// ---------|--------|-------
	// nil      | nil    | Branch exists and is tracked
	// nil      | !nil   | Branch is tracked, but was deleted out of band
	// !nil     | nil    | Branch is not tracked
	// !nil     | !nil   | Branch is not known to the repository
	if storeErr == nil && gitErr == nil {
		// Special case:
		// Branch exists and is tracked,
		// and was previously pushed to a remote,
		// but the remote branch reference has since been deleted.
		if resp.UpstreamBranch != "" {
			ok, err := s.verifyUpstreamBranchRef(ctx, name, resp.UpstreamBranch)
			if err != nil {
				s.log.Warn("Unable to verify upstream branch reference",
					"branch", name,
					"upstream", resp.UpstreamBranch,
					"error", err)
				resp.UpstreamBranch = ""
			}
			if !ok {
				// Upstream branch reference has been deleted.
				s.log.Debug("Upstream branch reference no longer valid",
					"branch", name,
					"upstream", resp.UpstreamBranch)

				resp.UpstreamBranch = ""
			}
		}

		out := &LookupBranchResponse{
			Base:            resp.Base,
			BaseHash:        resp.BaseHash,
			UpstreamBranch:  resp.UpstreamBranch,
			Head:            head,
			MergedDownstack: resp.MergedDownstack,
		}

		if resp.ChangeMetadata != nil {
			// TODO: This is ick. Service should have a Registry.
			if f, ok := s.forges.Lookup(resp.ChangeForge); !ok {
				s.log.Warn("Ignoring unknown forge requested in change metadata",
					"forge", resp.ChangeForge)
			} else {
				md, err := f.UnmarshalChangeMetadata(resp.ChangeMetadata)
				if err != nil {
					s.log.Warn("Corrupt change metadata associated with branch",
						"branch", name,
						"metadata", string(resp.ChangeMetadata),
						"error", err,
					)
				} else {
					out.Change = md
				}
			}
		}

		return out, nil
	}

	// Only one of these errors is set.
	if (storeErr != nil) != (gitErr != nil) {
		// Branch is not tracked, but exists in the repository.
		if storeErr != nil {
			return nil, fmt.Errorf("untracked branch %v: %w", name, storeErr)
		}

		if !errors.Is(gitErr, git.ErrNotExist) {
			return nil, fmt.Errorf("resolve head: %w", gitErr)
		}

		// Branch is tracked, but was deleted out of band.
		return nil, &DeletedBranchError{
			Name:     name,
			Base:     resp.Base,
			BaseHash: resp.BaseHash,
		}
	}

	// Both errors are set.
	// If the branch is not known to the repository,
	// return the git error.
	if errors.Is(gitErr, git.ErrNotExist) {
		return nil, fmt.Errorf("resolve head: %w", gitErr)
	}

	// Otherwise, something went wrong. Surface both errors.
	return nil, errors.Join(
		fmt.Errorf("untracked branch %v: %w", name, storeErr),
		fmt.Errorf("resolve head: %w", gitErr),
	)
}

// verifyUpstreamBranchRef verifies that the upstream branch reference is
// valid, and if not, it deletes that knowledge from the branch's state.
//
// That is, if $remote/$upstreamBranch does not exist,
// $branch's local state will forget about the upstream branch,
// but the branch will not be deleted.
//
// Returns true if the upstream branch reference is valid.
func (s *Service) verifyUpstreamBranchRef(ctx context.Context, branch, upstreamBranch string) (ok bool, err error) {
	remote, err := s.store.Remote()
	if err != nil {
		return false, nil // no remote, no upstream branch
	}
	if remote.Push == "" {
		return false, nil
	}

	upstreamRef := remote.Push + "/" + upstreamBranch
	if _, err := s.repo.PeelToCommit(ctx, upstreamRef); err == nil {
		return true, nil
	}

	tx := s.store.BeginBranchTx()
	var empty string
	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:           branch,
		UpstreamBranch: &empty,
	}); err != nil {
		return false, fmt.Errorf("update branch %v: %w", branch, err)
	}

	msg := fmt.Sprintf("upstream %q deleted out of band", upstreamRef)
	if err := tx.Commit(ctx, msg); err != nil {
		return false, fmt.Errorf("commit state: %w", err)
	}

	return false, nil
}

package spice

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
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
	MergedDownstack []jsontext.Value
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
	results, err := s.LookupBranches(ctx, []string{name})
	if err != nil {
		return nil, err
	}
	return results[0].Response, results[0].Err
}

// LookupBranchResult is the outcome for one name passed to [Service.LookupBranches].
type LookupBranchResult struct {
	// Response holds the branch information when Err is nil.
	Response *LookupBranchResponse

	// Err reports a branch-specific lookup failure, such as a deleted branch.
	Err error
}

// LookupBranches looks up tracked branches in the order requested.
// It returns one result per name, including repeated names.
// Branch-specific failures are reported in each result;
// the returned error reports a failure to load the batch or list local refs.
func (s *Service) LookupBranches(
	ctx context.Context,
	names []string,
) ([]LookupBranchResult, error) {
	if len(names) == 0 {
		return nil, nil
	}

	// Load tracked metadata together. Missing keys leave only their own slots
	// empty; other storage errors make the whole batch unusable.
	states, storeErr := s.store.LookupBranches(ctx, names)
	var missing []int
	if missingErr, ok := errors.AsType[*storage.MissingKeysError](storeErr); ok {
		missing = missingErr.Indexes
	} else if storeErr != nil {
		return nil, fmt.Errorf("load branch state: %w", storeErr)
	}

	// Resolve local heads in one call for a batch.
	// Single-name requests retain PeelToCommit's resolution and error behavior.
	heads := make(map[string]git.Hash)
	var headErr error
	if len(names) == 1 {
		heads[names[0]], headErr = s.repo.PeelToCommit(ctx, names[0])
	} else {
		for branch, err := range s.repo.LocalBranches(ctx, &git.LocalBranchesOptions{Patterns: names}) {
			if err != nil {
				return nil, fmt.Errorf("list local branches: %w", err)
			}
			heads[branch.Name] = branch.Hash
		}
	}

	// Combine metadata and refs to distinguish tracked, deleted, and untracked
	// branches. Missing indexes refer to positions, not unique branch names.
	results := make([]LookupBranchResult, len(names))
	for i, name := range names {
		var stateErr error
		if len(missing) > 0 && missing[0] == i {
			stateErr = state.ErrNotExist
			missing = missing[1:]
		}

		head, exists := heads[name]
		gitErr := headErr
		if !exists {
			gitErr = git.ErrNotExist
		}
		results[i].Response, results[i].Err = s.lookupBranchResponse(ctx, name, states[i], stateErr, head, gitErr)
	}
	return results, nil
}

func (s *Service) lookupBranchResponse(
	ctx context.Context,
	name string,
	resp *state.LookupResponse,
	storeErr error,
	head git.Hash,
	gitErr error,
) (*LookupBranchResponse, error) {
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
			codec, ok := s.forges.Lookup(resp.ChangeForge)
			if !ok {
				s.log.Warn("Ignoring unknown forge requested in change metadata",
					"forge", resp.ChangeForge)
			} else {
				md, err := codec.UnmarshalChangeMetadata(resp.ChangeMetadata)
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

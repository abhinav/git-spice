package forge

import (
	"context"
	"runtime"
	"sync"

	"go.abhg.dev/gs/internal/git"
)

// MatchedBranchChange is the compact change information used to associate an
// unsubmitted local branch with an existing forge change.
type MatchedBranchChange struct {
	// ID is the unique identifier for the change.
	ID ChangeID // required

	// State is the current state of the change.
	State ChangeState // required

	// HeadHash is the hash of the commit at the top of the change.
	HeadHash git.Hash // required
}

// MatchChangesToBranchesResult holds the lookup outcome for one input branch.
type MatchChangesToBranchesResult struct {
	// Changes contains matching changes in forge-defined preference order.
	// It is empty when no changes match the branch or Err is non-nil.
	Changes []*MatchedBranchChange

	// Err reports why this branch could not be queried.
	// Other result positions may still contain successful lookups.
	Err error
}

// MatchChangesToBranchesOptions configures [MatchChangesToBranches].
type MatchChangesToBranchesOptions struct {
	// PushRepository restricts matches to changes originating from this
	// repository. If nil, the target repository owns the head branch.
	PushRepository RepositoryID
}

// MatchChangesToBranches finds compact change information for branches.
// The returned slice has the same length and order as branches,
// and every result is non-nil.
// Each result reports the outcome for the branch at the same position.
//
// A repository may implement the following method:
//
//	MatchChangesToBranches(
//		context.Context,
//		[]string,
//		*MatchChangesToBranchesOptions,
//	) []*MatchChangesToBranchesResult
//
// MatchChangesToBranches delegates to that method when available.
// Otherwise, MatchChangesToBranches calls FindChangesByBranch with bounded
// concurrency.
func MatchChangesToBranches(
	ctx context.Context,
	repo Repository,
	branches []string,
	opts *MatchChangesToBranchesOptions,
) []*MatchChangesToBranchesResult {
	if matcher, ok := repo.(interface {
		MatchChangesToBranches(
			context.Context,
			[]string,
			*MatchChangesToBranchesOptions,
		) []*MatchChangesToBranchesResult
	}); ok {
		return matcher.MatchChangesToBranches(ctx, branches, opts)
	}

	var findOpts FindChangesOptions
	if opts != nil {
		findOpts.PushRepository = opts.PushRepository
	}
	findOpts.Limit = 10

	results := make([]*MatchChangesToBranchesResult, len(branches))
	branchIndexes := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(branches)) {
		wg.Go(func() {
			for i := range branchIndexes {
				changes, err := repo.FindChangesByBranch(ctx, branches[i], findOpts)
				if err != nil {
					results[i] = &MatchChangesToBranchesResult{Err: err}
					continue
				}

				matchedChanges := make([]*MatchedBranchChange, len(changes))
				for j, change := range changes {
					matchedChanges[j] = &MatchedBranchChange{
						ID:       change.ID,
						State:    change.State,
						HeadHash: change.HeadHash,
					}
				}
				results[i] = &MatchChangesToBranchesResult{
					Changes: matchedChanges,
				}
			}
		})
	}
	for i := range branches {
		branchIndexes <- i
	}
	close(branchIndexes)
	wg.Wait()
	return results
}

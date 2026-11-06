package submit

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/spice"
)

// submitStackUpdates carries provider base edits from the submit loop to the
// post-submit stack transaction. Existing changes keep their old provider base
// until the native provider plan has had a chance to unstack them safely.
type submitStackUpdates struct {
	requestedBranches []string
	deferredBases     []deferredBaseUpdate
}

type deferredBaseUpdate struct {
	branch     string
	repository forge.Repository
	change     forge.ChangeID
	base       string
}

// updateStackRepresentations synchronizes the forge-native stack and
// navigation comments from one view of the branch graph after submission.
// The independent forge updates run concurrently. Native-stack operation
// failures are warnings, while failures that prevent navigation comments are
// returned to the submit operation.
func (h *Handler) updateStackRepresentations(
	ctx context.Context,
	opts *Options,
	stackUpdates *submitStackUpdates,
	submittedBranches []string,
	completedBranches []string,
) error {
	if len(submittedBranches) == 0 || opts.DryRun {
		return nil
	}

	navCommentsEnabled := opts.NavComment != NavCommentNever
	var (
		remoteRepo forge.Repository
		stackRepo  forge.StackRepository
		graph      *spice.BranchGraph
	)
	if err := func() error {
		var err error
		remoteRepo, err = h.upstreamRepository(ctx)
		if err != nil {
			return fmt.Errorf("get remote repository: %w", err)
		}

		stackRepo, _ = remoteRepo.(forge.StackRepository)
		if stackRepo == nil && !navCommentsEnabled {
			return nil
		}

		graph, err = h.Service.BranchGraph(ctx, nil)
		if err != nil {
			return fmt.Errorf("build branch graph: %w", err)
		}
		return nil
	}(); err != nil {
		if navCommentsEnabled || len(stackUpdates.deferredBases) > 0 {
			return err
		}
		h.Log.Warn("Could not update stacks", "error", err)
		return nil
	}
	if graph == nil {
		return nil // neither representation is enabled
	}

	// The reloaded graph includes change metadata created during submission,
	// but only completedBranches finished all submission steps successfully.
	// Native reconciliation can dissolve existing membership.
	// If any requested branch is unfinished, exclude its tree so a partial
	// submission cannot remove existing native membership.
	completed := make(map[string]struct{}, len(completedBranches))
	for _, branch := range completedBranches {
		completed[branch] = struct{}{}
	}
	incompleteBottoms := make(map[string]struct{})
	for _, branch := range stackUpdates.requestedBranches {
		if _, ok := completed[branch]; !ok {
			incompleteBottoms[graph.Bottom(branch)] = struct{}{}
		}
	}
	completeBranches := make([]string, 0, len(completedBranches))
	for _, branch := range completedBranches {
		if _, ok := incompleteBottoms[graph.Bottom(branch)]; !ok {
			completeBranches = append(completeBranches, branch)
		}
	}

	var fallbackBases []deferredBaseUpdate
	for _, update := range stackUpdates.deferredBases {
		if _, ok := incompleteBottoms[graph.Bottom(update.branch)]; ok {
			fallbackBases = append(fallbackBases, update)
		}
	}
	// completeBranches now seeds only trees with no unfinished requested branches.
	// fallbackBases holds deferred edits for the excluded trees;
	// these will use ordinary per-change base updates instead of a native plan.

	// Navigation comment creation mutates change metadata. Project and plan the
	// native-stack request first so concurrent finalization owns only immutable
	// values from the shared graph snapshot.
	var stackChanges []forge.StackChange
	if stackRepo != nil {
		// Build membership from completed submissions and their downstack,
		// not the whole local tree. With lines connecting bases to aboves:
		//
		//   bottom ─┬─ middle ── submitted ── above
		//           └─ sibling ── cousin
		//
		// Submitting only "submitted" includes bottom, middle, and submitted.
		// Above, sibling, and cousin may have PRs, but this invocation has not
		// published their branches or prepared their base edits.
		// The forge still discovers any existing native membership through
		// the included PRs; it does not need those other PRs in this request.
		//
		// Inclusion permits native membership, not pending base edits.
		// Only completed submissions get BaseBranch; leaving it unset for
		// downstack context tells the forge to preserve the observed remote base.
		seen := make(map[string]struct{})
		for _, submittedBranch := range completeBranches {
			for name := range graph.Downstack(submittedBranch) {
				if _, ok := seen[name]; ok {
					break // this branch and its downstack have already been included
				}
				seen[name] = struct{}{}
				branch, _ := graph.Lookup(name)
				if branch.Change == nil {
					continue
				}

				change := forge.StackChange{Change: branch.Change.ChangeID()}
				_, submitted := completed[name]
				if submitted {
					change.BaseBranch = branch.Base
				}
				if base, ok := graph.Lookup(branch.Base); ok {
					if submitted {
						change.BaseBranch = cmp.Or(base.UpstreamBranch, change.BaseBranch)
					}
					if base.Change != nil {
						change.BaseChange = base.Change.ChangeID()
					}
				}
				stackChanges = append(stackChanges, change)
			}
		}
	}
	var stackPlan forge.StackUpdatePlan
	if len(stackChanges) > 0 {
		var err error
		stackPlan, err = stackRepo.PlanStackUpdate(ctx, stackChanges)
		switch {
		case errors.Is(err, forge.ErrUnsupported):
			// Base edits were deferred so the forge could unstack before retargeting.
			// Unsupported planning made no changes, so apply the ordinary edits instead.
			for _, update := range stackUpdates.deferredBases {
				if _, ok := incompleteBottoms[graph.Bottom(update.branch)]; !ok {
					fallbackBases = append(fallbackBases, update)
				}
			}
		case err != nil:
			h.Log.Warn("Could not plan stack update", "error", err)
		}
	}

	var wg sync.WaitGroup
	if stackPlan != nil {
		wg.Go(func() {
			if err := stackPlan.Execute(ctx); err != nil {
				h.Log.Warn("Could not update stacks", "error", err)
			}
		})
	}

	var fallbackErr error
	if len(fallbackBases) > 0 {
		wg.Go(func() {
			var errs []error
			for _, update := range fallbackBases {
				if err := update.repository.EditChange(ctx, update.change, forge.EditChangeOptions{
					Base: update.base,
				}); err != nil {
					errs = append(errs, fmt.Errorf("update %v base: %w", update.change, err))
				}
			}
			fallbackErr = errors.Join(errs...)
		})
	}

	var navCommentErr error
	if navCommentsEnabled {
		wg.Go(func() {
			navCommentErr = updateNavigationComments(
				ctx,
				navigationCommentUpdate{
					store:            h.Store,
					graph:            graph,
					log:              h.Log,
					remoteRepository: remoteRepo,

					when:          opts.NavComment,
					sync:          opts.NavCommentSync,
					downstack:     opts.NavCommentDownstack,
					marker:        opts.NavCommentMarker,
					layout:        opts.NavCommentLayout,
					trunkLink:     opts.NavCommentTrunkLink,
					trunkLinkText: opts.NavCommentTrunkLinkText,

					submittedBranches: submittedBranches,
					pushRepositoryID:  h.pushRepositoryID,
				},
			)
		})
	}
	wg.Wait()
	return errors.Join(navCommentErr, fallbackErr)
}

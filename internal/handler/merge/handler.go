// Package merge implements the downstack merge command.
package merge

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/handler/sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/ui"
)

// Store provides read access to the state store.
type Store interface {
	Trunk() string
}

// Service provides branch graph operations.
type Service interface {
	BranchGraph(
		context.Context,
		*spice.BranchGraphOptions,
	) (*spice.BranchGraph, error)
	VerifyRestacked(ctx context.Context, name string) error
}

// RestackHandler restacks branches after their bases are merged.
type RestackHandler interface {
	RestackBranch(context.Context, string) error
}

// SubmitHandler updates change requests after branch restacks.
type SubmitHandler interface {
	Submit(context.Context, *submit.Request) error
}

// SyncHandler updates trunk after each queue item merges.
type SyncHandler interface {
	SyncTrunk(context.Context, *sync.TrunkOptions) error
}

// GitRepository provides access to the Git repository.
type GitRepository interface {
	PeelToCommit(
		ctx context.Context, ref string,
	) (git.Hash, error)
	CommitAheadBehind(
		ctx context.Context, upstream, head string,
	) (ahead, behind int, err error)
}

// Request is a request to merge a branch and its downstack.
type Request struct {
	Branch string // required

	// NoWait skips polling for the merge to propagate.
	// Server-dependent cleanup is left to a later sync.
	NoWait bool

	// NoBranchCheck skips stale base validation.
	NoBranchCheck bool

	// Method selects the forge merge strategy.
	// Empty means use the forge default.
	Method forge.MergeMethod

	// BuildTimeout is the maximum time to wait
	// for CI checks to pass before each merge.
	// Zero means check once and fail if not ready.
	BuildTimeout time.Duration
}

// Handler merges change requests via the forge API.
type Handler struct {
	Log              *silog.Logger    // required
	View             ui.View          // required
	Store            Store            // required
	Service          Service          // required
	RemoteRepository forge.Repository // required
	Restack          RestackHandler   // required
	Submit           SubmitHandler    // required
	Sync             SyncHandler      // required

	// Cleanup dependencies:
	Repository GitRepository // required
	Remote     string        // required
}

// MergeDownstack merges the given branch
// and all its downstack ancestors bottom-up.
func (h *Handler) MergeDownstack(
	ctx context.Context, req *Request,
) error {
	plan, err := h.buildPlan(ctx, req)
	if err != nil {
		return err
	}

	if len(plan) == 0 {
		h.Log.Info("No open changes to merge.")
		return nil
	}

	if err := h.confirm(plan); err != nil {
		return err
	}

	return h.executePlan(ctx, plan, req)
}

// mergeItem is one queue item in a downstack merge plan.
//
// buildPlan fills branch, changeID, and upstreamBranch from the branch graph.
// validateSynced later fills headHash after it verifies the local branch
// matches its upstream ref.
type mergeItem struct {
	// branch is the local branch to merge.
	branch string

	// changeID identifies the forge Change Request to merge.
	changeID forge.ChangeID

	// upstreamBranch is the remote branch used for push-safety checks.
	upstreamBranch string

	// headHash is passed to MergeChange for server-side assertion.
	headHash git.Hash
}

// buildPlan snapshots repository state into the local merge queue.
//
// The plan is ordered bottom-up,
// contains only open Change Requests,
// and has local push-safety metadata ready for execution.
func (h *Handler) buildPlan(
	ctx context.Context, req *Request,
) ([]*mergeItem, error) {
	graph, err := h.Service.BranchGraph(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("build branch graph: %w", err)
	}

	// Build the queue bottom-up because each merge changes
	// the base of the branch above it.
	downstack := slices.Collect(graph.Downstack(req.Branch))
	slices.Reverse(downstack)

	items := make([]*mergeItem, 0, len(downstack))
	ids := make([]forge.ChangeID, 0, len(downstack))
	for _, name := range downstack {
		branch, ok := graph.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("branch %q is not tracked", name)
		}
		if branch.Change == nil {
			return nil, fmt.Errorf(
				"branch %q has no published change request",
				name,
			)
		}

		item := &mergeItem{
			branch:         name,
			changeID:       branch.Change.ChangeID(),
			upstreamBranch: branch.UpstreamBranch,
		}
		items = append(items, item)
		ids = append(ids, item.changeID)
	}

	statuses, err := h.RemoteRepository.ChangeStatuses(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("query change states: %w", err)
	}

	// Drop already-merged changes from the queue,
	// but stop if any downstack Change Request was closed without merging.
	plan := items[:0]
	for i, item := range items {
		switch statuses[i].State {
		case forge.ChangeMerged:
			h.Log.Infof("%s (%v): already merged, skipping",
				item.branch, item.changeID)
		case forge.ChangeClosed:
			return nil, fmt.Errorf(
				"branch %q (%v) is closed, cannot merge",
				item.branch, item.changeID,
			)
		case forge.ChangeOpen:
			plan = append(plan, item)
		}
	}

	if req.NoWait && len(plan) > 1 {
		return nil, fmt.Errorf(
			"--no-wait can merge only one branch; "+
				"got %d branches",
			len(plan),
		)
	}

	if err := h.validateSynced(ctx, plan); err != nil {
		return nil, fmt.Errorf("validate branch sync: %w", err)
	}

	if !req.NoBranchCheck {
		if err := h.validateFreshBases(ctx, graph, req.Branch); err != nil {
			return nil, fmt.Errorf("validate stale bases: %w", err)
		}
	}

	return plan, nil
}

// validateSynced checks that all branches in the merge plan
// are in sync with the remote.
// If any branch has unpushed or missing commits,
// the merge is halted with an error listing all such branches.
//
// For branches that are in sync,
// their headHash is captured for use as the expected SHA
// in the forge merge request.
func (h *Handler) validateSynced(
	ctx context.Context, items []*mergeItem,
) error {
	type outOfSync struct {
		name          string
		ahead, behind int
	}

	var problems []outOfSync
	for _, item := range items {
		if item.upstreamBranch == "" {
			continue
		}

		remoteRef := h.Remote + "/" + item.upstreamBranch
		ahead, behind, err := h.Repository.CommitAheadBehind(
			ctx, remoteRef, item.branch,
		)
		if err != nil {
			// Remote ref may not exist (e.g., pruned).
			// Skip rather than false-positive,
			// but report that the push-safety check was incomplete.
			h.Log.Warn("Unable to verify branch push status",
				"branch", item.branch,
				"remoteRef", remoteRef,
				"error", err)
			continue
		}

		if ahead > 0 || behind > 0 {
			problems = append(problems, outOfSync{
				name:   item.branch,
				ahead:  ahead,
				behind: behind,
			})
			continue
		}

		// Branch is in sync; capture head hash
		// for the forge merge SHA assertion.
		head, err := h.Repository.PeelToCommit(
			ctx, item.branch,
		)
		if err != nil {
			h.Log.Warn("Unable to resolve head",
				"branch", item.branch, "error", err)
			continue
		}
		item.headHash = head
	}

	if len(problems) == 0 {
		return nil
	}

	var msg strings.Builder
	fmt.Fprintf(&msg,
		"the following branch(es) are out of sync"+
			" with the remote:\n")
	for _, b := range problems {
		switch {
		case b.ahead > 0 && b.behind > 0:
			fmt.Fprintf(&msg,
				"  %s (%d unpushed, %d behind remote)\n",
				b.name, b.ahead, b.behind)
		case b.ahead > 0:
			fmt.Fprintf(&msg,
				"  %s (%d unpushed)\n",
				b.name, b.ahead)
		default:
			fmt.Fprintf(&msg,
				"  %s (%d behind remote)\n",
				b.name, b.behind)
		}
	}
	fmt.Fprintf(&msg,
		"Push with 'gs branch submit' or discard with\n"+
			"'git reset --hard %s/<branch>' for each branch.",
		h.Remote)
	return errors.New(msg.String())
}

func (h *Handler) confirm(plan []*mergeItem) error {
	if !ui.Interactive(h.View) {
		return nil
	}

	var desc strings.Builder
	for _, item := range plan {
		fmt.Fprintf(&desc,
			"  %s (%v)\n", item.branch, item.changeID)
	}

	proceed := true
	prompt := ui.NewConfirm().
		WithTitle(
			fmt.Sprintf(
				"Merge %d change(s) bottom-up?",
				len(plan),
			),
		).
		WithDescription(desc.String()).
		WithValue(&proceed)
	if err := ui.Run(h.View, prompt); err != nil {
		return fmt.Errorf("run prompt: %w", err)
	}

	if !proceed {
		return errors.New("merge aborted")
	}
	return nil
}

func (h *Handler) executePlan(
	ctx context.Context, plan []*mergeItem, req *Request,
) error {
	trunk := h.Store.Trunk()

	for i, item := range plan {
		lastItem := i == len(plan)-1

		// After each lower branch merges,
		// prepare the next branch on top of updated trunk state.
		if i > 0 {
			if err := h.prepareForMerge(ctx, item); err != nil {
				return fmt.Errorf("prepare %q: %w", item.branch, err)
			}
		}

		// The forge will only merge a change that targets trunk.
		// Re-check immediately before each merge because prior queue items
		// and repo sync may have changed the server-side base.
		change, err := h.ensureTargetsTrunk(
			ctx, item, trunk,
		)
		if err != nil {
			return fmt.Errorf(
				"ensure %q targets trunk: %w",
				item.branch, err,
			)
		}

		// CI is checked after retargeting and restacking
		// so each merge waits on the exact change being merged.
		if err := h.awaitChecks(
			ctx, item, req.BuildTimeout,
		); err != nil {
			return fmt.Errorf(
				"wait for checks on %q: %w",
				item.branch, err,
			)
		}

		h.Log.Infof(
			"%s: merging %v: %s",
			item.branch, item.changeID, change.URL,
		)
		if err := h.RemoteRepository.MergeChange(
			ctx, item.changeID,
			forge.MergeChangeOptions{
				Method:   req.Method,
				HeadHash: item.headHash,
			},
		); err != nil {
			return fmt.Errorf("merge %q: %w", item.branch, err)
		}

		if req.NoWait {
			continue
		}

		// Wait until the forge reports the merge.
		if err := h.awaitMerged(ctx, item); err != nil {
			return fmt.Errorf("await merge of %q: %w",
				item.branch, err)
		}

		// SyncTrunk updates trunk,
		// deletes merged branches,
		// and retargets their upstacks.
		if err := h.Sync.SyncTrunk(ctx, &sync.TrunkOptions{
			ClosedChanges: sync.ClosedChangesIgnore,
		}); err != nil {
			if !lastItem {
				return fmt.Errorf("sync trunk: %w", err)
			}
			h.Log.Warn("Unable to sync trunk after merge",
				"error", err)
		}
	}

	h.Log.Infof("All %d change(s) merged.", len(plan))
	return nil
}

func (h *Handler) validateFreshBases(
	ctx context.Context,
	graph *spice.BranchGraph,
	branch string,
) error {
	staleBases, err := spice.FindStaleBases(ctx, graph,
		func(context.Context) (forge.Repository, error) {
			return h.RemoteRepository, nil
		}, []string{branch})
	if err != nil {
		return fmt.Errorf("find stale bases: %w", err)
	}
	if len(staleBases) == 0 {
		return nil
	}

	for _, staleBase := range staleBases {
		h.Log.Warn("Branch has stale base",
			"branch", staleBase.Branch,
			"base", staleBase.Base,
		)
	}
	return fmt.Errorf(
		"%d branches with stale bases were found; "+
			"run 'gs repo sync' first, "+
			"or use --no-branch-check to merge anyway",
		len(staleBases),
	)
}

// prepareForMerge advances an item in the local merge queue
// before it is merged.
//
// This is intentionally outside delete cleanup:
// the merge queue owns the user-visible restack and submit
// needed before the branch can be merged.
func (h *Handler) prepareForMerge(
	ctx context.Context,
	item *mergeItem,
) error {
	if err := h.Service.VerifyRestacked(ctx, item.branch); err != nil {
		var restackErr *spice.BranchNeedsRestackError
		if !errors.As(err, &restackErr) {
			return fmt.Errorf("verify restacked: %w", err)
		}

		if err := h.Restack.RestackBranch(ctx, item.branch); err != nil {
			return fmt.Errorf("restack branch: %w", err)
		}

		if err := h.Submit.Submit(ctx, &submit.Request{
			Branch: item.branch,
			Options: &submit.Options{
				// Publish keeps this on the normal Change Request update path.
				// UpdateOnly still prevents creating a new Change Request.
				Publish: true,

				UpdateOnly: new(true),
			},
		}); err != nil {
			return fmt.Errorf("submit branch update: %w", err)
		}
	}

	head, err := h.Repository.PeelToCommit(ctx, item.branch)
	if err != nil {
		return fmt.Errorf("resolve updated head: %w", err)
	}
	item.headHash = head
	return nil
}

// awaitChecks polls until CI checks pass for the given change.
// Uses truncated exponential backoff.
func (h *Handler) awaitChecks(
	ctx context.Context,
	item *mergeItem,
	timeout time.Duration,
) error {
	const (
		_baseDelay = 10 * time.Second
		_maxDelay  = 30 * time.Second
	)

	return h.awaitChecksWithDelay(
		ctx, item, timeout, _baseDelay, _maxDelay,
	)
}

func (h *Handler) awaitChecksWithDelay(
	ctx context.Context,
	item *mergeItem,
	timeout, baseDelay, maxDelay time.Duration,
) error {
	delay := baseDelay
	for attempt := 0; ; attempt++ {
		state, err := h.RemoteRepository.ChangeChecksState(
			ctx, item.changeID,
		)
		if err != nil {
			return fmt.Errorf("query checks: %w", err)
		}
		if state == forge.ChecksPassed {
			return nil
		}
		if state == forge.ChecksFailed {
			return fmt.Errorf(
				"CI checks failed for %q", item.branch,
			)
		}

		if timeout == 0 {
			return fmt.Errorf(
				"CI checks pending for %q (build-timeout=0)",
				item.branch,
			)
		}
		if attempt == 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}

		h.Log.Infof("%s: waiting for CI checks", item.branch)
		if err := sleep(ctx, delay); err != nil {
			return fmt.Errorf(
				"timed out waiting for CI on %q",
				item.branch,
			)
		}
		delay = min(delay*2, maxDelay)
	}
}

// ensureTargetsTrunk verifies a change targets trunk
// on the forge, retargeting if needed.
func (h *Handler) ensureTargetsTrunk(
	ctx context.Context,
	item *mergeItem,
	trunk string,
) (*forge.FindChangeItem, error) {
	change, err := h.RemoteRepository.FindChangeByID(
		ctx, item.changeID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"check base of %q: %w", item.branch, err,
		)
	}

	if change.BaseName == trunk {
		return change, nil
	}

	if err := h.retargetChange(ctx, item, trunk); err != nil {
		return nil, err
	}
	return change, nil
}

// awaitMerged polls until the given change shows as merged.
// Uses exponential backoff starting at 500ms, capped at 8s.
func (h *Handler) awaitMerged(
	ctx context.Context, item *mergeItem,
) error {
	const (
		_initialDelay = 500 * time.Millisecond
		_maxDelay     = 8 * time.Second
		_timeout      = 2 * time.Minute
	)

	ctx, cancel := context.WithTimeout(ctx, _timeout)
	defer cancel()

	// TODO: This only waits for the immediate change to reach
	// the merged state.
	// Server-side merge queues and richer merge workflows
	// need a more expressive wait state.
	delay := _initialDelay
	for {
		statuses, err := h.RemoteRepository.ChangeStatuses(
			ctx, []forge.ChangeID{item.changeID},
		)
		if err != nil {
			return fmt.Errorf("poll state: %w", err)
		}

		if statuses[0].State == forge.ChangeMerged {
			return nil
		}

		h.Log.Debugf("%s: waiting for merge to settle",
			item.branch)
		if err := sleep(ctx, delay); err != nil {
			return fmt.Errorf(
				"timed out waiting for %q to merge",
				item.branch,
			)
		}

		delay = min(delay*2, _maxDelay)
	}
}

// retargetChange updates a change's base to trunk.
func (h *Handler) retargetChange(
	ctx context.Context, item *mergeItem, trunk string,
) error {
	h.Log.Infof("%s: retargeting %v onto %s",
		item.branch, item.changeID, trunk)
	err := h.RemoteRepository.EditChange(
		ctx, item.changeID,
		forge.EditChangeOptions{Base: trunk},
	)
	if err != nil {
		return fmt.Errorf("retarget %q to %q: %w",
			item.branch, trunk, err)
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Package merge implements forge-backed merge commands.
package merge

import (
	"cmp"
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

// Options controls behavior shared by forge-backed merge commands.
type Options struct {
	// Method selects the forge merge strategy.
	// Empty means use the configured or forge default.
	Method forge.MergeMethod `placeholder:"METHOD" config:"merge.method" help:"Preferred merge method. One of 'merge', 'squash', and 'rebase'."`

	// MergeReadinessTimeout is the maximum time to wait for the forge
	// to report that a change is ready to merge.
	// Zero means check once and fail if merge readiness is not reached.
	MergeReadinessTimeout time.Duration `name:"ready-timeout" config:"merge.readyTimeout" default:"30m" help:"Max time to wait for merge readiness before each merge. 0 means check once."`
}

// DownstackMergeOptions controls downstack merge behavior.
type DownstackMergeOptions struct {
	Options

	// NoBranchCheck skips stale base validation before merging.
	NoBranchCheck bool `help:"Skip stale base validation before merging."`
}

// DownstackMergeRequest asks Handler to merge a branch
// and its downstack ancestors bottom-up.
type DownstackMergeRequest struct {
	Branch string // required

	Options *DownstackMergeOptions // optional

	// BranchGraph reuses branch graph data already loaded by the caller.
	BranchGraph *spice.BranchGraph // optional
}

// BranchMergeRequest asks Handler to merge one branch
// that is configured directly on trunk.
type BranchMergeRequest struct {
	Branch string // required

	Options *Options // optional
}

// StackMergeOptions controls stack merge behavior.
type StackMergeOptions struct {
	Options

	// NoBranchCheck skips stale base validation before merging.
	NoBranchCheck bool `help:"Skip stale base validation before merging."`

	// FailFast stops the merge queue after the first branch failure.
	FailFast bool `help:"Stop the merge queue after the first branch failure."`
}

// StackMergeRequest asks Handler to merge a branch,
// its downstack branches down to trunk,
// and its upstack branches.
type StackMergeRequest struct {
	Branch string // required

	Options *StackMergeOptions // optional
}

// Handler merges change requests via the forge API.
type Handler struct {
	Log                *silog.Logger      // required
	View               ui.View            // required
	Store              Store              // required
	Service            Service            // required
	RemoteRepository   forge.Repository   // required
	RemoteRepositoryID forge.RepositoryID // required
	Restack            RestackHandler     // required
	Submit             SubmitHandler      // required
	Sync               SyncHandler        // required

	// Cleanup dependencies:
	Repository GitRepository // required
	Remote     string        // required
}

// MergeDownstack merges the given branch
// and all its downstack ancestors bottom-up.
func (h *Handler) MergeDownstack(
	ctx context.Context, req *DownstackMergeRequest,
) error {
	opts := cmp.Or(req.Options, &DownstackMergeOptions{})
	plan, err := h.buildPlan(ctx, req)
	if err != nil {
		return err
	}

	if len(plan.items) == 0 {
		h.Log.Info("No open changes to merge.")
		return nil
	}

	if err := h.confirm(
		plan.items,
		fmt.Sprintf("Merge %d change(s) bottom-up?", len(plan.items)),
	); err != nil {
		return err
	}

	return h.executePlan(ctx, plan.items, mergeExecutionOptions{
		Method:                opts.Method,
		MergeReadinessTimeout: opts.MergeReadinessTimeout,
		SyncBeforeStart:       plan.syncBeforeStart,
	})
}

// MergeBranch merges one branch that is configured directly on trunk.
func (h *Handler) MergeBranch(
	ctx context.Context, req *BranchMergeRequest,
) error {
	opts := cmp.Or(req.Options, &Options{})
	graph, err := h.Service.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("build branch graph: %w", err)
	}

	branch, ok := graph.Lookup(req.Branch)
	if !ok {
		return fmt.Errorf("branch %q is not tracked", req.Branch)
	}
	trunk := h.Store.Trunk()
	if branch.Base != trunk {
		return fmt.Errorf(
			"branch %q is based on %q, not trunk; "+
				"use 'gs downstack merge --branch %s' "+
				"to merge stack branches bottom-up",
			req.Branch, branch.Base, req.Branch,
		)
	}

	return h.MergeDownstack(ctx, &DownstackMergeRequest{
		Branch: req.Branch,
		Options: &DownstackMergeOptions{
			Options: *opts,
		},
		BranchGraph: graph,
	})
}

// MergeStack merges the given branch,
// its downstack branches down to trunk,
// and its upstack branches.
func (h *Handler) MergeStack(
	ctx context.Context, req *StackMergeRequest,
) error {
	opts := cmp.Or(req.Options, &StackMergeOptions{})
	graph, err := h.Service.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("build branch graph: %w", err)
	}

	plan, err := h.buildPlanFromBranches(ctx, mergePlanRequest{
		Graph:             graph,
		Branches:          slices.Collect(graph.Stack(req.Branch)),
		NoBranchCheck:     opts.NoBranchCheck,
		IgnoreUnsubmitted: true,
	})
	if err != nil {
		return err
	}

	if len(plan.items) == 0 {
		h.Log.Info("No open changes to merge.")
		return nil
	}

	if err := h.confirm(
		plan.items,
		fmt.Sprintf("Merge %d change(s)?", len(plan.items)),
	); err != nil {
		return err
	}

	return h.executePlan(ctx, plan.items, mergeExecutionOptions{
		Method:                opts.Method,
		MergeReadinessTimeout: opts.MergeReadinessTimeout,
		FailFast:              opts.FailFast,
		SyncBeforeStart:       plan.syncBeforeStart,
	})
}

// mergeItem is one queue item in a downstack merge plan.
//
// buildPlanFromBranches fills branch, base, changeID, and upstreamBranch
// from the branch graph for either a linear downstack
// or a graph-shaped stack merge.
// validateSynced later fills headHash after it verifies
// the local branch matches its upstream ref.
type mergeItem struct {
	// branch is the local branch to merge.
	branch string

	// base is the configured base branch observed
	// when the plan was built.
	base string

	// changeID identifies the forge Change Request to merge.
	changeID forge.ChangeID

	// upstreamBranch is the remote branch used for push-safety checks.
	upstreamBranch string

	// headHash is passed to MergeChange for server-side assertion.
	headHash git.Hash

	// mergeURL is the forge URL displayed when requesting the merge.
	mergeURL string
}

// mergePlan is the prepared queue plus repository state
// that execution must reconcile before the queue starts.
type mergePlan struct {
	items []*mergeItem

	// syncBeforeStart is true when plan construction observed
	// already-merged changes while still-open changes remain.
	// The queue barrier only runs after successful queue items,
	// so execution must sync once before the first item
	// to retarget surviving upstack changes around those skipped bases.
	syncBeforeStart bool
}

// buildPlan snapshots repository state into the local merge queue.
//
// The plan is ordered bottom-up,
// contains only open Change Requests,
// and has local push-safety metadata ready for execution.
func (h *Handler) buildPlan(
	ctx context.Context, req *DownstackMergeRequest,
) (mergePlan, error) {
	opts := cmp.Or(req.Options, &DownstackMergeOptions{})
	graph := req.BranchGraph
	if graph == nil {
		var err error
		graph, err = h.Service.BranchGraph(ctx, nil)
		if err != nil {
			return mergePlan{}, fmt.Errorf("build branch graph: %w", err)
		}
	}

	// Build the queue bottom-up because each merge changes
	// the base of the branch above it.
	downstack := slices.Collect(graph.Downstack(req.Branch))
	slices.Reverse(downstack)

	return h.buildPlanFromBranches(ctx, mergePlanRequest{
		Graph:         graph,
		Branches:      downstack,
		NoBranchCheck: opts.NoBranchCheck,
	})
}

// mergePlanRequest selects the local branches that become merge queue items.
//
// Branches provides the prompt order.
// The merge queue still enforces base dependencies before execution.
type mergePlanRequest struct {
	Graph *spice.BranchGraph // required

	Branches []string // required

	NoBranchCheck     bool
	IgnoreUnsubmitted bool
}

func (h *Handler) buildPlanFromBranches(
	ctx context.Context, req mergePlanRequest,
) (mergePlan, error) {
	items := make([]*mergeItem, 0, len(req.Branches))
	ids := make([]forge.ChangeID, 0, len(req.Branches))
	for _, name := range req.Branches {
		branch, ok := req.Graph.Lookup(name)
		if !ok {
			return mergePlan{}, fmt.Errorf("branch %q is not tracked", name)
		}
		if branch.Change == nil {
			if req.IgnoreUnsubmitted {
				h.Log.Infof("%s: no published change request, skipping", name)
				continue
			}
			return mergePlan{}, fmt.Errorf(
				"branch %q has no published change request",
				name,
			)
		}

		item := &mergeItem{
			branch:         name,
			base:           branch.Base,
			changeID:       branch.Change.ChangeID(),
			upstreamBranch: branch.UpstreamBranch,
			mergeURL:       h.RemoteRepositoryID.ChangeURL(branch.Change.ChangeID()),
		}
		items = append(items, item)
		ids = append(ids, item.changeID)
	}
	if len(items) == 0 {
		return mergePlan{}, nil
	}

	statuses, err := h.RemoteRepository.ChangeStatuses(ctx, ids)
	if err != nil {
		return mergePlan{}, fmt.Errorf("query change states: %w", err)
	}

	// Drop already-merged changes from the queue,
	// but stop if any Change Request was closed without merging.
	plan := items[:0]
	var sawMerged bool
	for i, item := range items {
		switch statuses[i].State {
		case forge.ChangeMerged:
			sawMerged = true
			h.Log.Infof("%s (%v): already merged, skipping",
				item.branch, item.changeID)
		case forge.ChangeClosed:
			return mergePlan{}, fmt.Errorf(
				"branch %q (%v) is closed, cannot merge",
				item.branch, item.changeID,
			)
		case forge.ChangeOpen:
			plan = append(plan, item)
		}
	}

	if err := h.validateSynced(ctx, plan); err != nil {
		return mergePlan{}, fmt.Errorf("validate branch sync: %w", err)
	}

	if !req.NoBranchCheck {
		branches := req.Branches
		if req.IgnoreUnsubmitted {
			branches = make([]string, 0, len(plan))
			for _, item := range plan {
				branches = append(branches, item.branch)
			}
		}
		if err := h.validateFreshBases(
			ctx, req.Graph, branches,
		); err != nil {
			return mergePlan{}, fmt.Errorf("validate stale bases: %w", err)
		}
	}

	return mergePlan{
		items:           plan,
		syncBeforeStart: sawMerged && len(plan) > 0,
	}, nil
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

func (h *Handler) confirm(plan []*mergeItem, title string) error {
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
		WithTitle(title).
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

type mergeExecutionOptions struct {
	Method                forge.MergeMethod
	MergeReadinessTimeout time.Duration
	FailFast              bool
	SyncBeforeStart       bool
}

func (h *Handler) executePlan(
	ctx context.Context,
	plan []*mergeItem,
	opts mergeExecutionOptions,
) (err error) {
	if opts.SyncBeforeStart {
		if err := h.Sync.SyncTrunk(ctx, &sync.TrunkOptions{
			ClosedChanges: sync.ClosedChangesIgnore,
		}); err != nil {
			return fmt.Errorf("sync trunk: %w", err)
		}
	}

	var progress mergeProgress
	if runner, ok := h.View.(ui.ModelView); ok {
		widgetProgress := newWidgetMergeProgress(
			runner, h.View.Theme(),
		)
		progress = mergeProgressGroup{
			widgetProgress,
			newLogMergeProgress(h.Log),
		}
		ctx = widgetProgress.Start(ctx, plan)
		defer func() {
			err = errors.Join(err, widgetProgress.Finish())
		}()
	} else {
		progress = newLogMergeProgress(h.Log)
	}

	err = (&mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,

		Service: h.Service,
		Restack: h.Restack,
		Submit:  h.Submit,
		Sync:    h.Sync,

		Progress: progress,

		Trunk:                 h.Store.Trunk(),
		MergeReadinessTimeout: opts.MergeReadinessTimeout,
		Method:                opts.Method,
		FailFast:              opts.FailFast,
	}).Execute(ctx, plan)
	if err != nil {
		return err
	}

	h.Log.Infof("All %d change(s) merged.", len(plan))
	return nil
}

func (h *Handler) validateFreshBases(
	ctx context.Context,
	graph *spice.BranchGraph,
	branches []string,
) error {
	staleBases, err := spice.FindStaleBases(ctx, graph,
		func(context.Context) (forge.Repository, error) {
			return h.RemoteRepository, nil
		}, branches)
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
func (e *mergePlanExecutor) prepareForMerge(
	ctx context.Context,
	item *mergeItem,
) error {
	if err := e.Service.VerifyRestacked(ctx, item.branch); err != nil {
		var restackErr *spice.BranchNeedsRestackError
		if !errors.As(err, &restackErr) {
			return fmt.Errorf("verify restacked: %w", err)
		}

		if err := e.Restack.RestackBranch(ctx, item.branch); err != nil {
			return fmt.Errorf("restack branch: %w", err)
		}

		if err := e.Submit.Submit(ctx, &submit.Request{
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

	head, err := e.Repository.PeelToCommit(ctx, item.branch)
	if err != nil {
		return fmt.Errorf("resolve updated head: %w", err)
	}
	item.headHash = head
	return nil
}

// awaitChangeHead waits until the forge reports the same change head
// that the merge loop is about to merge.
func (e *mergePlanExecutor) awaitChangeHead(
	ctx context.Context,
	item *mergeItem,
) error {
	const (
		_baseDelay = 10 * time.Second
		_maxDelay  = 30 * time.Second

		// Head visibility is a forge catch-up wait,
		// not a merge readiness policy wait.
		// Keep this bounded separately from --ready-timeout
		// so repository rules and CI get the configured readiness budget.
		_timeout = time.Minute
	)

	return e.awaitChangeHeadWithDelay(
		ctx, item, _timeout, _baseDelay, _maxDelay,
	)
}

func (e *mergePlanExecutor) awaitChangeHeadWithDelay(
	ctx context.Context,
	item *mergeItem,
	timeout, baseDelay, maxDelay time.Duration,
) error {
	if item.headHash == "" {
		return nil
	}
	hashMatches := func(got git.Hash) bool {
		return got != "" &&
			(item.headHash == got ||
				strings.HasPrefix(item.headHash.String(), got.String()) ||
				strings.HasPrefix(got.String(), item.headHash.String()))
	}

	delay := baseDelay
	for attempt := 0; ; attempt++ {
		statuses, err := e.RemoteRepository.ChangeStatuses(
			ctx, []forge.ChangeID{item.changeID},
		)
		if err != nil {
			return fmt.Errorf("query change head: %w", err)
		}
		if len(statuses) == 0 {
			return errors.New("forge returned no change status")
		}
		if hashMatches(statuses[0].HeadHash) {
			return nil
		}

		if timeout == 0 {
			return fmt.Errorf(
				"change head is still %s, want %s",
				statuses[0].HeadHash, item.headHash,
			)
		}
		if attempt == 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}

		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressWaitingForForgeHead,
			Item: item,
		})
		if err := sleep(ctx, delay); err != nil {
			return fmt.Errorf("HEAD did not update after %v", timeout)
		}
		delay = min(delay*2, maxDelay)
	}
}

// awaitMergeability polls until the forge reports that the change is
// ready to merge.
// Uses truncated exponential backoff.
func (e *mergePlanExecutor) awaitMergeability(
	ctx context.Context,
	item *mergeItem,
) error {
	const (
		_baseDelay = 10 * time.Second
		_maxDelay  = 30 * time.Second
	)

	return e.awaitMergeabilityWithDelay(
		ctx, item, e.MergeReadinessTimeout, _baseDelay, _maxDelay,
	)
}

func (e *mergePlanExecutor) awaitMergeabilityWithDelay(
	ctx context.Context,
	item *mergeItem,
	timeout, baseDelay, maxDelay time.Duration,
) error {
	delay := baseDelay
	for attempt := 0; ; attempt++ {
		mergeability, err := e.RemoteRepository.ChangeMergeability(
			ctx, item.changeID,
		)
		if err != nil {
			return fmt.Errorf("check merge readiness: %w", err)
		}
		switch mergeability.State {
		case forge.ChangeMergeabilityReady:
			e.Progress.Event(mergeProgressEvent{
				Kind: mergeProgressMergeabilityReady,
				Item: item,
			})
			return nil
		case forge.ChangeMergeabilityWaiting:
			if timeout == 0 {
				return fmt.Errorf("not ready after %v", timeout)
			}
		case forge.ChangeMergeabilityBlocked:
			return fmt.Errorf("blocked: %s", mergeability.Reason)
		case forge.ChangeMergeabilityUnknown:
			return errors.New("unknown state")
		case forge.ChangeMergeabilityUnsupported:
			return errors.New("unknown state")
		default:
			return fmt.Errorf("unknown state: %v", mergeability.State)
		}
		if attempt == 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}

		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressWaitingForMergeability,
			Item: item,
		})
		if err := sleep(ctx, delay); err != nil {
			return fmt.Errorf("not ready after %v", timeout)
		}
		delay = min(delay*2, maxDelay)
	}
}

// awaitMerged polls until the given change shows as merged.
// Uses exponential backoff starting at 500ms, capped at 8s.
func (e *mergePlanExecutor) awaitMerged(
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
		statuses, err := e.RemoteRepository.ChangeStatuses(
			ctx, []forge.ChangeID{item.changeID},
		)
		if err != nil {
			return fmt.Errorf("poll state: %w", err)
		}

		if statuses[0].State == forge.ChangeMerged {
			return nil
		}

		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressWaitingForMerge,
			Item: item,
		})
		if err := sleep(ctx, delay); err != nil {
			return errors.New("timed out waiting for merge")
		}

		delay = min(delay*2, _maxDelay)
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

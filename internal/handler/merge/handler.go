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

	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/handler/sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/ui"
)

const defaultMergeTimeout = 2 * time.Minute

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
	RestackBranch(ctx context.Context, req *restack.BranchRequest) error
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

// ScriptRunner is the command execution boundary
// used to run user-provided scripts.
type ScriptRunner interface {
	Run(context.Context, *scriptrun.RunRequest) (*scriptrun.RunResult, error)
}

// Options controls behavior shared by forge-backed merge commands.
type Options struct {
	// Method selects the forge merge strategy.
	// Empty means use the configured or forge default.
	Method forge.MergeMethod `placeholder:"METHOD" config:"merge.method" help:"Preferred merge method. One of 'merge', 'squash', and 'rebase'."`

	// Command requests merges through a user-defined command.
	// Empty means use the forge merge API.
	Command string `hidden:"" config:"merge.command" help:"Command to request merge instead of using the forge merge API."`

	// ReadyCommand checks merge readiness through a user-defined command.
	// Empty means use forge readiness.
	ReadyCommand string `hidden:"" config:"merge.ready.command" help:"Command to check merge readiness instead of using the forge."`

	// ReadyTimeout is the maximum time to wait
	// for the configured readiness provider.
	// Zero means check once and fail if merge readiness is not reached.
	ReadyTimeout time.Duration `name:"ready-timeout" config:"merge.ready.timeout" default:"30m" help:"Max time to wait for merge readiness before each merge. 0 means check once."`

	// MergeTimeout is the maximum time to wait for the forge
	// to report that a change is merged after requesting merge.
	MergeTimeout time.Duration `name:"merge-timeout" config:"merge.timeout" default:"2m" help:"Max time to wait for merge completion after requesting merge."`

	// FailFast stops scheduling remaining merge queue work
	// after the first branch failure.
	FailFast bool `help:"Stop scheduling remaining merge queue work after the first branch failure."`
}

// DownstackMergeRequest asks Handler to merge each requested branch
// and its downstack ancestors bottom-up.
type DownstackMergeRequest struct {
	Branches []string // required

	Options *Options // optional

	// BranchGraph reuses branch graph data already loaded by the caller.
	BranchGraph *spice.BranchGraph // optional
}

// BranchMergeRequest asks Handler to merge the selected branches.
type BranchMergeRequest struct {
	Branches []string // required

	Options *Options // optional
}

// StackMergeRequest asks Handler to merge each requested branch,
// its downstack branches down to trunk,
// and its upstack branches.
type StackMergeRequest struct {
	Branches []string // required

	Options *Options // optional
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

	ScriptRunner ScriptRunner // required

	// Cleanup dependencies:
	Repository GitRepository // required
	Remote     string        // required
}

// MergeDownstack merges the given branch
// and all its downstack ancestors bottom-up.
func (h *Handler) MergeDownstack(
	ctx context.Context, req *DownstackMergeRequest,
) error {
	opts := cmp.Or(req.Options, &Options{})
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
		Method:          opts.Method,
		Command:         opts.Command,
		ReadyCommand:    opts.ReadyCommand,
		ReadyTimeout:    opts.ReadyTimeout,
		MergeTimeout:    opts.MergeTimeout,
		FailFast:        opts.FailFast,
		SyncBeforeStart: plan.syncBeforeStart,
	})
}

// MergeBranch merges the selected branches without expanding their scopes.
func (h *Handler) MergeBranch(
	ctx context.Context, req *BranchMergeRequest,
) error {
	opts := cmp.Or(req.Options, &Options{})
	graph, err := h.Service.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("build branch graph: %w", err)
	}

	selected := make(map[string]struct{}, len(req.Branches))
	for _, name := range req.Branches {
		selected[name] = struct{}{}
	}

	// Branch merge treats --branch as an exact branch selection.
	// Every selected branch must have its non-trunk bases selected too,
	// so the selected set itself forms the path to trunk.
	for _, name := range req.Branches {
		for current := name; current != graph.Trunk(); {
			branch, ok := graph.Lookup(current)
			if !ok {
				return fmt.Errorf("branch %q is not tracked", current)
			}

			if _, ok := selected[current]; !ok {
				return fmt.Errorf(
					"branch %q requires selected base %q",
					name, current,
				)
			}
			current = branch.Base
		}
	}

	plan, err := h.buildPlanFromBranches(ctx, mergePlanRequest{
		Graph:    graph,
		Branches: req.Branches,
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
		fmt.Sprintf("Merge %d change(s) bottom-up?", len(plan.items)),
	); err != nil {
		return err
	}

	return h.executePlan(ctx, plan.items, mergeExecutionOptions{
		Method:          opts.Method,
		Command:         opts.Command,
		ReadyCommand:    opts.ReadyCommand,
		ReadyTimeout:    opts.ReadyTimeout,
		MergeTimeout:    opts.MergeTimeout,
		FailFast:        opts.FailFast,
		SyncBeforeStart: plan.syncBeforeStart,
	})
}

// MergeStack merges the given branch,
// its downstack branches down to trunk,
// and its upstack branches.
func (h *Handler) MergeStack(
	ctx context.Context, req *StackMergeRequest,
) error {
	opts := cmp.Or(req.Options, &Options{})
	graph, err := h.Service.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("build branch graph: %w", err)
	}

	var branches []string
	for _, name := range req.Branches {
		if _, ok := graph.Lookup(name); !ok {
			return fmt.Errorf("branch %q is not tracked", name)
		}
		for branchName := range graph.Stack(name) {
			branch, ok := graph.Lookup(branchName)
			if !ok {
				branches = append(branches, branchName)
				continue
			}
			if branch.Change == nil {
				h.Log.Infof(
					"%s: no published change request, skipping",
					branchName,
				)
				continue
			}
			branches = append(branches, branchName)
		}
	}

	plan, err := h.buildPlanFromBranches(ctx, mergePlanRequest{
		Graph:    graph,
		Branches: branches,
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
		Method:          opts.Method,
		Command:         opts.Command,
		ReadyCommand:    opts.ReadyCommand,
		ReadyTimeout:    opts.ReadyTimeout,
		MergeTimeout:    opts.MergeTimeout,
		FailFast:        opts.FailFast,
		SyncBeforeStart: plan.syncBeforeStart,
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
	graph := req.BranchGraph
	if graph == nil {
		var err error
		graph, err = h.Service.BranchGraph(ctx, nil)
		if err != nil {
			return mergePlan{}, fmt.Errorf("build branch graph: %w", err)
		}
	}

	var branches []string
	for _, name := range req.Branches {
		if _, ok := graph.Lookup(name); !ok {
			return mergePlan{}, fmt.Errorf("branch %q is not tracked", name)
		}

		// Build each downstack bottom-up because each merge changes
		// the base of the branch above it.
		downstack := slices.Collect(graph.Downstack(name))
		slices.Reverse(downstack)
		branches = append(branches, downstack...)
	}

	return h.buildPlanFromBranches(ctx, mergePlanRequest{
		Graph:    graph,
		Branches: branches,
	})
}

// mergePlanRequest selects the local branches that become merge queue items.
//
// Branches provides the prompt order.
// Scope expansion may append the same branch more than once;
// buildPlanFromBranches normalizes duplicates before querying
// forge state or constructing merge queue items.
// The merge queue still enforces base dependencies before execution.
type mergePlanRequest struct {
	Graph *spice.BranchGraph // required

	Branches []string // required
}

func (h *Handler) buildPlanFromBranches(
	ctx context.Context, req mergePlanRequest,
) (mergePlan, error) {
	branches := uniqueBranches(req.Branches)
	items := make([]*mergeItem, 0, len(branches))
	ids := make([]forge.ChangeID, 0, len(branches))
	for _, name := range branches {
		branch, ok := req.Graph.Lookup(name)
		if !ok {
			return mergePlan{}, fmt.Errorf("branch %q is not tracked", name)
		}
		if branch.Change == nil {
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

	if err := h.validateFreshBases(
		ctx, req.Graph, branches,
	); err != nil {
		return mergePlan{}, fmt.Errorf("validate stale bases: %w", err)
	}

	return mergePlan{
		items:           plan,
		syncBeforeStart: sawMerged && len(plan) > 0,
	}, nil
}

func uniqueBranches(branches []string) []string {
	if len(branches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(branches))
	out := make([]string, 0, len(branches))
	for _, branch := range branches {
		if _, ok := seen[branch]; ok {
			continue
		}
		seen[branch] = struct{}{}
		out = append(out, branch)
	}
	return out
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
	Method          forge.MergeMethod
	Command         string
	ReadyCommand    string
	ReadyTimeout    time.Duration
	MergeTimeout    time.Duration
	FailFast        bool
	SyncBeforeStart bool
}

func (opts mergeExecutionOptions) mergeTimeout() time.Duration {
	if opts.MergeTimeout == 0 {
		return defaultMergeTimeout
	}
	return opts.MergeTimeout
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

	var _commandRunner *commandRunner
	getCommandRunner := func() *commandRunner {
		if _commandRunner != nil {
			return _commandRunner
		}

		_commandRunner = &commandRunner{
			Log:        h.Log,
			Repository: h.RemoteRepository,
			ForgeID:    h.RemoteRepository.Forge().ID(),
			Trunk:      h.Store.Trunk(),
			Runner:     h.ScriptRunner,
		}
		return _commandRunner
	}

	mergeRequester := mergeRequester(&forgeMergeRequester{
		Repository: h.RemoteRepository,
		Method:     opts.Method,
	})
	if opts.Command != "" {
		mergeRequester = &commandMergeRequester{
			Runner: getCommandRunner(),
			Script: opts.Command,
		}
	}

	readinessChecker := readinessChecker(&forgeReadinessChecker{
		Repository: h.RemoteRepository,
	})
	if opts.ReadyCommand != "" {
		readinessChecker = &commandReadinessChecker{
			Runner: getCommandRunner(),
			Script: opts.ReadyCommand,
		}
	}

	err = (&mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,

		Service: h.Service,
		Restack: h.Restack,
		Submit:  h.Submit,
		Sync:    h.Sync,

		Progress:         progress,
		MergeRequester:   mergeRequester,
		ReadinessChecker: readinessChecker,

		Trunk:        h.Store.Trunk(),
		ReadyTimeout: opts.ReadyTimeout,
		MergeTimeout: opts.mergeTimeout(),
		Method:       opts.Method,
		FailFast:     opts.FailFast,
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
			"run 'gs repo sync' first",
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

		if err := e.Restack.RestackBranch(ctx, &restack.BranchRequest{
			Branch: item.branch,
		}); err != nil {
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
		ctx, item, e.ReadyTimeout, _baseDelay, _maxDelay,
	)
}

func (e *mergePlanExecutor) awaitMergeabilityWithDelay(
	ctx context.Context,
	item *mergeItem,
	timeout, baseDelay, maxDelay time.Duration,
) error {
	must.NotBeNilf(e.ReadinessChecker, "merge: ReadinessChecker is required")

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	delay := baseDelay
	for attempt := 0; ; attempt++ {
		mergeability, err := e.ReadinessChecker.CheckMergeItemReady(ctx, item)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("not ready after %v", timeout)
			}
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
	)

	ctx, cancel := context.WithTimeout(ctx, e.MergeTimeout)
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

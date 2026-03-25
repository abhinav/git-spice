// Package merge implements the branch merge command.
package merge

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	branchdel "go.abhg.dev/gs/internal/handler/delete"

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
	ListDownstack(ctx context.Context, start string) ([]string, error)
	LookupBranch(
		ctx context.Context, name string,
	) (*spice.LookupBranchResponse, error)
}

// DeleteHandler allows deleting branches.
type DeleteHandler interface {
	DeleteBranches(context.Context, *branchdel.Request) error
}

// GitRepository provides access to the Git repository.
type GitRepository interface {
	PeelToCommit(
		ctx context.Context, ref string,
	) (git.Hash, error)
	DeleteBranch(
		ctx context.Context, branch string,
		opts git.BranchDeleteOptions,
	) error
	Fetch(
		ctx context.Context, opts git.FetchOptions,
	) error
	CommitAheadBehind(
		ctx context.Context, upstream, head string,
	) (ahead, behind int, err error)
}

// Request is a request to merge a branch and its downstack.
type Request struct {
	Branch string // required

	// NoWait skips polling for each merge to propagate.
	// Retargeting and cleanup still happen.
	NoWait bool

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

	// Cleanup dependencies:
	Delete     DeleteHandler // required
	Repository GitRepository // required
	Remote     string        // required
}

// MergeDownstack merges the given branch
// and all its downstack ancestors bottom-up.
func (h *Handler) MergeDownstack(
	ctx context.Context, req *Request,
) error {
	plan, err := h.buildPlan(ctx, req.Branch)
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

// mergeItem is a single branch+change to merge.
type mergeItem struct {
	branch         string
	changeID       forge.ChangeID
	upstreamBranch string // name pushed to remote

	// headHash is set by verifySynced right before the merge.
	// It is passed to MergeChange for server-side assertion.
	headHash git.Hash
}

func (h *Handler) buildPlan(
	ctx context.Context, branch string,
) ([]mergeItem, error) {
	downstack, err := h.Service.ListDownstack(ctx, branch)
	if err != nil {
		return nil, fmt.Errorf("list downstack: %w", err)
	}

	// ListDownstack returns top-first; reverse for bottom-up.
	slices.Reverse(downstack)

	items, err := h.resolveChanges(ctx, downstack)
	if err != nil {
		return nil, err
	}

	plan, err := h.filterMerged(ctx, items)
	if err != nil {
		return nil, err
	}

	if err := h.validateSynced(ctx, plan); err != nil {
		return nil, err
	}

	return plan, nil
}

func (h *Handler) resolveChanges(
	ctx context.Context, branches []string,
) ([]mergeItem, error) {
	var items []mergeItem
	for _, name := range branches {
		resp, err := h.Service.LookupBranch(ctx, name)
		if err != nil {
			return nil, fmt.Errorf(
				"lookup branch %q: %w", name, err,
			)
		}

		if resp.Change == nil {
			return nil, fmt.Errorf(
				"branch %q has no published change request",
				name,
			)
		}

		items = append(items, mergeItem{
			branch:         name,
			changeID:       resp.Change.ChangeID(),
			upstreamBranch: resp.UpstreamBranch,
		})
	}
	return items, nil
}

func (h *Handler) filterMerged(
	ctx context.Context, items []mergeItem,
) ([]mergeItem, error) {
	ids := make([]forge.ChangeID, len(items))
	for i, item := range items {
		ids[i] = item.changeID
	}

	statuses, err := h.RemoteRepository.ChangeStatuses(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("query change states: %w", err)
	}

	var plan []mergeItem
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
	ctx context.Context, items []mergeItem,
) error {
	type outOfSync struct {
		name          string
		ahead, behind int
	}

	var problems []outOfSync
	for i := range items {
		item := &items[i]
		if item.upstreamBranch == "" {
			continue
		}

		remoteRef := h.Remote + "/" + item.upstreamBranch
		ahead, behind, err := h.Repository.CommitAheadBehind(
			ctx, remoteRef, item.branch,
		)
		if err != nil {
			// Remote ref may not exist (e.g., pruned).
			// Skip rather than false-positive.
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

func (h *Handler) confirm(plan []mergeItem) error {
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
	ctx context.Context, plan []mergeItem, req *Request,
) error {
	trunk := h.Store.Trunk()

	// Verify the first item targets trunk before merging.
	if err := h.ensureTargetsTrunk(
		ctx, plan[0], trunk,
	); err != nil {
		return err
	}

	for i := range plan {
		if err := h.mergeOne(
			ctx, plan, i, trunk, req,
		); err != nil {
			return err
		}
	}

	h.Log.Infof("All %d change(s) merged.", len(plan))
	return nil
}

func (h *Handler) mergeOne(
	ctx context.Context,
	plan []mergeItem,
	idx int,
	trunk string,
	req *Request,
) error {
	if err := h.waitAndMerge(
		ctx, plan[idx], req.BuildTimeout,
	); err != nil {
		return err
	}

	return h.postMerge(ctx, plan, idx, trunk, req.NoWait)
}

func (h *Handler) waitAndMerge(
	ctx context.Context,
	item mergeItem,
	buildTimeout time.Duration,
) error {
	if err := h.awaitChecks(
		ctx, item, buildTimeout,
	); err != nil {
		return err
	}

	h.Log.Infof(
		"Merging %s (%v)...", item.branch, item.changeID,
	)
	if err := h.RemoteRepository.MergeChange(
		ctx, item.changeID,
		forge.MergeChangeOptions{HeadHash: item.headHash},
	); err != nil {
		return fmt.Errorf("merge %q: %w", item.branch, err)
	}
	return nil
}

func (h *Handler) postMerge(
	ctx context.Context,
	plan []mergeItem,
	idx int,
	trunk string,
	noWait bool,
) error {
	item := plan[idx]
	lastItem := idx == len(plan)-1

	// Wait for merge to propagate (unless --no-wait).
	if !noWait {
		if err := h.awaitMerged(ctx, item); err != nil {
			return fmt.Errorf("await merge of %q: %w",
				item.branch, err)
		}
	}

	// Fetch trunk so that the local ref includes
	// the just-merged commit.
	// Without this, cleanup would rebase upstack branches
	// onto a stale trunk, dropping the merged changes.
	if err := h.fetchTrunk(ctx, trunk); err != nil {
		h.Log.Warn("Unable to fetch trunk after merge",
			"error", err)
	}

	h.cleanupMerged(ctx, item)

	if lastItem {
		return nil
	}

	// Retarget next PR to trunk.
	// If --no-wait, merge may not have propagated yet;
	// log a warning on failure instead of aborting.
	if err := h.retargetChange(
		ctx, plan[idx+1], trunk,
	); err != nil && noWait {
		h.Log.Warn("Retarget may have failed "+
			"(merge may not have propagated yet)",
			"branch", plan[idx+1].branch, "error", err)
		return nil
	} else if err != nil {
		return err
	}
	return nil
}

// awaitChecks polls until CI checks pass for the given change.
// Uses truncated exponential backoff.
func (h *Handler) awaitChecks(
	ctx context.Context,
	item mergeItem,
	timeout time.Duration,
) error {
	const (
		_baseDelay = 10 * time.Second
		_maxDelay  = 30 * time.Second
	)

	state, err := h.checkOnce(ctx, item)
	if err != nil {
		return err
	}
	if state == forge.ChecksPassed {
		return nil
	}
	if state == forge.ChecksFailed {
		return fmt.Errorf(
			"CI checks failed for %q", item.branch,
		)
	}

	// Checks are pending. If timeout is zero, fail immediately.
	if timeout == 0 {
		return fmt.Errorf(
			"CI checks pending for %q (build-timeout=0)",
			item.branch,
		)
	}

	return h.pollChecks(ctx, item, timeout, _baseDelay, _maxDelay)
}

func (h *Handler) pollChecks(
	ctx context.Context,
	item mergeItem,
	timeout, baseDelay, maxDelay time.Duration,
) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	delay := baseDelay
	for {
		h.Log.Infof(
			"Waiting for CI checks on %s...", item.branch,
		)
		if err := sleep(ctx, delay); err != nil {
			return fmt.Errorf(
				"timed out waiting for CI on %q",
				item.branch,
			)
		}

		state, err := h.checkOnce(ctx, item)
		if err != nil {
			return err
		}
		if state == forge.ChecksPassed {
			return nil
		}
		if state == forge.ChecksFailed {
			return fmt.Errorf(
				"CI checks failed for %q", item.branch,
			)
		}

		delay = min(delay*2, maxDelay)
	}
}

func (h *Handler) checkOnce(
	ctx context.Context, item mergeItem,
) (forge.ChecksState, error) {
	state, err := h.RemoteRepository.ChangeChecksStatus(
		ctx, item.changeID,
	)
	if err != nil {
		return 0, fmt.Errorf(
			"query checks for %q: %w", item.branch, err,
		)
	}
	return state, nil
}

// ensureTargetsTrunk verifies a change targets trunk
// on the forge, retargeting if needed.
func (h *Handler) ensureTargetsTrunk(
	ctx context.Context,
	item mergeItem,
	trunk string,
) error {
	change, err := h.RemoteRepository.FindChangeByID(
		ctx, item.changeID,
	)
	if err != nil {
		return fmt.Errorf(
			"check base of %q: %w", item.branch, err,
		)
	}

	if change.BaseName == trunk {
		return nil
	}

	return h.retargetChange(ctx, item, trunk)
}

// awaitMerged polls until the given change shows as merged.
// Uses exponential backoff starting at 500ms, capped at 8s.
func (h *Handler) awaitMerged(
	ctx context.Context, item mergeItem,
) error {
	const (
		_initialDelay = 500 * time.Millisecond
		_maxDelay     = 8 * time.Second
		_timeout      = 2 * time.Minute
	)

	ctx, cancel := context.WithTimeout(ctx, _timeout)
	defer cancel()

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

		h.Log.Debugf("Waiting for %s to settle...",
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
	ctx context.Context, item mergeItem, trunk string,
) error {
	h.Log.Infof("Retargeting %s to %s...",
		item.branch, trunk)
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

// fetchTrunk fetches the trunk branch from the remote,
// updating the local ref to include newly merged commits.
func (h *Handler) fetchTrunk(
	ctx context.Context, trunk string,
) error {
	return h.Repository.Fetch(ctx, git.FetchOptions{
		Remote: h.Remote,
		Refspecs: []git.Refspec{
			git.Refspec(trunk + ":" + trunk),
		},
	})
}

// cleanupMerged deletes the local and remote tracking
// branches for a branch that was just merged.
func (h *Handler) cleanupMerged(
	ctx context.Context, item mergeItem,
) {
	h.Log.Infof("Cleaning up %s...", item.branch)

	err := h.Delete.DeleteBranches(ctx, &branchdel.Request{
		Branches: []string{item.branch},
		Force:    true,
	})
	if err != nil {
		h.Log.Warn("Unable to delete local branch",
			"branch", item.branch, "error", err)
	}

	h.deleteRemoteTracking(ctx, item)
}

// deleteRemoteTracking removes the remote tracking ref
// for the given branch if it exists.
func (h *Handler) deleteRemoteTracking(
	ctx context.Context, item mergeItem,
) {
	upstream := item.upstreamBranch
	if upstream == "" {
		upstream = item.branch
	}

	remoteBranch := h.Remote + "/" + upstream
	if _, err := h.Repository.PeelToCommit(
		ctx, remoteBranch,
	); err != nil {
		return // does not exist
	}

	err := h.Repository.DeleteBranch(
		ctx, remoteBranch,
		git.BranchDeleteOptions{Remote: true},
	)
	if err != nil {
		h.Log.Warn(
			"Unable to delete remote tracking branch",
			"branch", remoteBranch, "error", err,
		)
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

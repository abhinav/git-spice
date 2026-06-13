// Package integration implements the gs integration command tree.
//
// Integration branches are repo-scoped singletons that combine the tips
// of multiple tracked branches by sequentially merging them onto trunk.
// They are deliberately separate from tracked stack branches:
// they do not receive PRs and are invisible to gs branch commands.
package integration

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

//go:generate mockgen -typed -destination mocks_test.go -package integration -write_package_comment=false . GitRepository,GitWorktree,Store,Service

// GitRepository is the subset of [git.Repository] used by the handler.
type GitRepository interface {
	PeelToCommit(ctx context.Context, ref string) (git.Hash, error)
	ListRemoteRefs(ctx context.Context, remote string, opts *git.ListRemoteRefsOptions) iter.Seq2[git.RemoteRef, error]
}

var _ GitRepository = (*git.Repository)(nil)

// GitWorktree is the subset of [git.Worktree] used by the handler.
type GitWorktree interface {
	CurrentBranch(ctx context.Context) (string, error)
	CheckoutBranch(ctx context.Context, branch string) error
	CheckoutNewBranch(ctx context.Context, req git.CheckoutNewBranchRequest) error
	Merge(ctx context.Context, opts git.MergeOptions) error
	IsClean(ctx context.Context) (bool, error)
	Push(ctx context.Context, opts git.PushOptions) error
}

var _ GitWorktree = (*git.Worktree)(nil)

// Store is the subset of [state.Store] used by the handler.
type Store interface {
	Trunk() string
	Remote() (state.Remote, error)
	Integration(ctx context.Context) (*state.IntegrationInfo, error)
	SetIntegration(ctx context.Context, info *state.IntegrationInfo) error
	PendingIntegrationRebuild(ctx context.Context) (*state.IntegrationRebuild, error)
	SetPendingIntegrationRebuild(ctx context.Context, rb *state.IntegrationRebuild) error
	ClearPendingIntegrationRebuild(ctx context.Context) error
}

var _ Store = (*state.Store)(nil)

// Service is the subset of [spice.Service] used by the handler.
type Service interface {
	LookupBranch(ctx context.Context, name string) (*spice.LookupBranchResponse, error)
}

var _ Service = (*spice.Service)(nil)

// Handler implements integration branch operations.
type Handler struct {
	Log        *silog.Logger // required
	Repository GitRepository // required
	Worktree   GitWorktree   // required
	Store      Store         // required
	Service    Service       // required
}

// ErrNotConfigured indicates that no integration branch is configured.
var ErrNotConfigured = errors.New("no integration branch configured")

// ErrAlreadyConfigured indicates that an integration branch is already
// configured. There can be at most one per repo.
var ErrAlreadyConfigured = errors.New("integration branch already configured")

// CreateRequest is a request to create the integration branch
// configuration.
type CreateRequest struct {
	// Name is the local branch name of the integration branch.
	Name string // required

	// UpstreamBranch is the remote-side branch name.
	// Defaults to Name if empty.
	UpstreamBranch string

	// Tips lists tracked branches whose tips compose the integration
	// branch.
	Tips []string
}

// Create sets up the singleton integration branch configuration.
// Returns [ErrAlreadyConfigured] if one is already configured.
func (h *Handler) Create(ctx context.Context, req *CreateRequest) error {
	if req.Name == "" {
		return errors.New("integration branch name is required")
	}
	if req.Name == h.Store.Trunk() {
		return errors.New("integration branch name must not equal trunk")
	}

	switch _, err := h.Store.Integration(ctx); {
	case err == nil:
		return ErrAlreadyConfigured
	case errors.Is(err, state.ErrNotExist):
		// ok
	default:
		return fmt.Errorf("get integration: %w", err)
	}

	tips := make([]state.IntegrationTip, 0, len(req.Tips))
	seen := make(map[string]struct{}, len(req.Tips))
	for _, name := range req.Tips {
		if err := h.validateTipName(ctx, req.Name, name, seen); err != nil {
			return err
		}
		tips = append(tips, state.IntegrationTip{Name: name})
		seen[name] = struct{}{}
	}

	info := &state.IntegrationInfo{
		Name:           req.Name,
		UpstreamBranch: req.UpstreamBranch,
		Tips:           tips,
	}
	if err := h.Store.SetIntegration(ctx, info); err != nil {
		return fmt.Errorf("save integration: %w", err)
	}
	return nil
}

// Checkout switches the worktree to the configured integration branch.
// Returns [ErrNotConfigured] if no integration is configured, or an
// error if the integration branch does not yet exist (e.g., has never
// been rebuilt).
func (h *Handler) Checkout(ctx context.Context) error {
	info, err := h.loadConfigured(ctx)
	if err != nil {
		return err
	}

	if _, err := h.Repository.PeelToCommit(ctx, info.Name); err != nil {
		return fmt.Errorf("integration branch %q does not exist; run 'gs integration rebuild' first", info.Name)
	}

	if err := h.Worktree.CheckoutBranch(ctx, info.Name); err != nil {
		return fmt.Errorf("checkout: %w", err)
	}
	return nil
}

// Delete clears the integration branch configuration.
// The underlying git branch (if any) is not touched.
func (h *Handler) Delete(ctx context.Context) error {
	switch _, err := h.Store.Integration(ctx); {
	case err == nil:
		// ok
	case errors.Is(err, state.ErrNotExist):
		return ErrNotConfigured
	default:
		return fmt.Errorf("get integration: %w", err)
	}

	if err := h.Store.SetIntegration(ctx, nil); err != nil {
		return fmt.Errorf("clear integration: %w", err)
	}
	return nil
}

// AddTip adds a branch to the integration tip list.
func (h *Handler) AddTip(ctx context.Context, branch string) error {
	info, err := h.loadConfigured(ctx)
	if err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(info.Tips))
	for _, tip := range info.Tips {
		seen[tip.Name] = struct{}{}
	}
	if _, exists := seen[branch]; exists {
		return fmt.Errorf("tip %q is already configured", branch)
	}
	if err := h.validateTipName(ctx, info.Name, branch, seen); err != nil {
		return err
	}

	info.Tips = append(info.Tips, state.IntegrationTip{Name: branch})
	if err := h.Store.SetIntegration(ctx, info); err != nil {
		return fmt.Errorf("save integration: %w", err)
	}
	return nil
}

// RemoveTip removes a branch from the integration tip list.
func (h *Handler) RemoveTip(ctx context.Context, branch string) error {
	info, err := h.loadConfigured(ctx)
	if err != nil {
		return err
	}

	idx := slices.IndexFunc(info.Tips, func(t state.IntegrationTip) bool {
		return t.Name == branch
	})
	if idx < 0 {
		return fmt.Errorf("tip %q is not configured", branch)
	}

	info.Tips = slices.Delete(info.Tips, idx, idx+1)
	if err := h.Store.SetIntegration(ctx, info); err != nil {
		return fmt.Errorf("save integration: %w", err)
	}
	return nil
}

// Status describes the current state of the integration branch.
type Status struct {
	// Name is the integration branch name.
	Name string

	// UpstreamBranch is the remote branch name.
	UpstreamBranch string

	// LastPushedHash is the hash recorded at the last successful push.
	LastPushedHash git.Hash

	// Tips lists each configured tip with its recorded and current
	// hashes. Drift = StoredHash != CurrentHash.
	Tips []TipStatus
}

// TipStatus reports drift for a single tip.
type TipStatus struct {
	Name        string
	StoredHash  git.Hash
	CurrentHash git.Hash
	// Missing is true if the branch no longer exists in the repository.
	Missing bool
}

// Drifted reports whether the tip's current hash differs from the stored
// hash. A missing tip is also considered drifted.
func (s TipStatus) Drifted() bool {
	return s.Missing || s.CurrentHash != s.StoredHash
}

// Show returns the current configuration and per-tip drift status.
// Returns [ErrNotConfigured] if no integration is configured.
func (h *Handler) Show(ctx context.Context) (*Status, error) {
	info, err := h.loadConfigured(ctx)
	if err != nil {
		return nil, err
	}

	out := &Status{
		Name:           info.Name,
		UpstreamBranch: info.UpstreamBranch,
		LastPushedHash: info.LastPushedHash,
		Tips:           make([]TipStatus, 0, len(info.Tips)),
	}
	for _, tip := range info.Tips {
		ts := TipStatus{Name: tip.Name, StoredHash: tip.Hash}
		hash, err := h.Repository.PeelToCommit(ctx, tip.Name)
		if err != nil {
			ts.Missing = true
		} else {
			ts.CurrentHash = hash
		}
		out.Tips = append(out.Tips, ts)
	}
	return out, nil
}

// RebuildResult summarizes a successful Rebuild operation.
type RebuildResult struct {
	// Name is the integration branch name.
	Name string

	// TipHashes holds the hash of each tip used in the rebuild.
	TipHashes []git.Hash
}

// ConflictError indicates that a rebuild was interrupted by a merge
// conflict. The conflict is left in the worktree for the user to
// resolve.
type ConflictError struct {
	// Tip is the name of the tip whose merge conflicted.
	Tip string

	// Paths are the files with unresolved conflicts.
	Paths []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("merge of tip %q conflicted in %d file(s)", e.Tip, len(e.Paths))
}

// Rebuild regenerates the integration branch by sequentially merging
// each configured tip onto trunk.
//
// If a previous rebuild was interrupted by a conflict and the user has
// since resolved it (committed via 'git merge --continue'), Rebuild
// resumes from where it left off.
//
// On conflict, the merge is left in the worktree for the user to
// resolve, and a [*ConflictError] is returned. After resolving and
// committing the merge, the user re-runs Rebuild to continue.
func (h *Handler) Rebuild(ctx context.Context) (*RebuildResult, error) {
	info, err := h.loadConfigured(ctx)
	if err != nil {
		return nil, err
	}

	pending, err := h.Store.PendingIntegrationRebuild(ctx)
	switch {
	case err == nil:
		if pending.Integration != info.Name {
			h.Log.Warnf("Discarding pending rebuild for stale integration %q", pending.Integration)
			if err := h.Store.ClearPendingIntegrationRebuild(ctx); err != nil {
				return nil, fmt.Errorf("clear stale pending rebuild: %w", err)
			}
			pending = nil
		}
	case errors.Is(err, state.ErrNotExist):
		pending = nil
	default:
		return nil, fmt.Errorf("check pending rebuild: %w", err)
	}

	if pending != nil {
		return h.resumeRebuild(ctx, info, pending)
	}
	return h.freshRebuild(ctx, info)
}

func (h *Handler) freshRebuild(ctx context.Context, info *state.IntegrationInfo) (*RebuildResult, error) {
	currentBranch, err := h.Worktree.CurrentBranch(ctx)
	if err != nil {
		return nil, fmt.Errorf("current branch: %w", err)
	}

	clean, err := h.Worktree.IsClean(ctx)
	if err != nil {
		return nil, fmt.Errorf("check worktree: %w", err)
	}
	if !clean {
		return nil, errors.New("worktree has uncommitted changes; commit or stash them first")
	}

	trunk := h.Store.Trunk()
	trunkHash, err := h.Repository.PeelToCommit(ctx, trunk)
	if err != nil {
		return nil, fmt.Errorf("resolve trunk %q: %w", trunk, err)
	}

	tips := make([]state.IntegrationTip, 0, len(info.Tips))
	for _, tip := range info.Tips {
		if _, err := h.Service.LookupBranch(ctx, tip.Name); err != nil {
			return nil, fmt.Errorf("tip %q: %w", tip.Name, err)
		}
		hash, err := h.Repository.PeelToCommit(ctx, tip.Name)
		if err != nil {
			return nil, fmt.Errorf("resolve tip %q: %w", tip.Name, err)
		}
		tips = append(tips, state.IntegrationTip{Name: tip.Name, Hash: hash})
	}

	if err := h.Worktree.CheckoutNewBranch(ctx, git.CheckoutNewBranchRequest{
		Name:       info.Name,
		StartPoint: trunkHash.String(),
		Force:      true,
	}); err != nil {
		return nil, fmt.Errorf("create integration branch: %w", err)
	}

	return h.runMerges(ctx, info, tips, 0, currentBranch)
}

func (h *Handler) resumeRebuild(
	ctx context.Context,
	info *state.IntegrationInfo,
	pending *state.IntegrationRebuild,
) (*RebuildResult, error) {
	clean, err := h.Worktree.IsClean(ctx)
	if err != nil {
		return nil, fmt.Errorf("check worktree: %w", err)
	}
	if !clean {
		return nil, errors.New("worktree has uncommitted changes (likely an unfinished merge); resolve and 'git merge --continue', or 'git merge --abort'")
	}

	currentBranch, err := h.Worktree.CurrentBranch(ctx)
	if err != nil {
		return nil, fmt.Errorf("current branch: %w", err)
	}
	if currentBranch != info.Name {
		if err := h.Worktree.CheckoutBranch(ctx, info.Name); err != nil {
			return nil, fmt.Errorf("switch to integration branch: %w", err)
		}
	}

	h.Log.Infof("Resuming integration rebuild at tip %d of %d",
		pending.NextTipIndex+1, len(pending.Tips))
	return h.runMerges(ctx, info, pending.Tips, pending.NextTipIndex, currentBranch)
}

// runMerges merges tips[start:] onto HEAD, finalizes the rebuild on
// success, and saves pending state + returns a [*ConflictError] on
// conflict (without aborting the merge).
func (h *Handler) runMerges(
	ctx context.Context,
	info *state.IntegrationInfo,
	tips []state.IntegrationTip,
	start int,
	originalBranch string,
) (*RebuildResult, error) {
	for i := start; i < len(tips); i++ {
		tip := tips[i]
		err := h.Worktree.Merge(ctx, git.MergeOptions{
			Refs:          []string{tip.Hash.String()},
			NoFF:          true,
			Message:       fmt.Sprintf("Merge %s into %s", tip.Name, info.Name),
			EnableRerere:  true,
			LeaveConflict: true,
		})
		if err == nil {
			continue
		}

		conflict := new(git.MergeConflictError)
		if errors.As(err, &conflict) {
			if saveErr := h.Store.SetPendingIntegrationRebuild(ctx, &state.IntegrationRebuild{
				Integration:  info.Name,
				Tips:         tips,
				NextTipIndex: i + 1,
			}); saveErr != nil {
				h.Log.Warnf("save pending rebuild: %v", saveErr)
			}
			return nil, &ConflictError{Tip: tip.Name, Paths: conflict.ConflictPaths}
		}
		return nil, fmt.Errorf("merge tip %q: %w", tip.Name, err)
	}

	info.Tips = tips
	if err := h.Store.SetIntegration(ctx, info); err != nil {
		return nil, fmt.Errorf("save integration state: %w", err)
	}
	if err := h.Store.ClearPendingIntegrationRebuild(ctx); err != nil {
		h.Log.Warnf("clear pending rebuild: %v", err)
	}

	if originalBranch != "" && originalBranch != info.Name {
		if err := h.Worktree.CheckoutBranch(ctx, originalBranch); err != nil {
			h.Log.Warnf("Could not restore branch %q: %v", originalBranch, err)
		}
	}

	hashes := make([]git.Hash, len(tips))
	for i, t := range tips {
		hashes[i] = t.Hash
	}
	return &RebuildResult{
		Name:      info.Name,
		TipHashes: hashes,
	}, nil
}

// PushRejectedError indicates that [Handler.Submit] could not push
// because the remote integration branch already exists at a hash that
// the local state does not recognize as previously-pushed.
//
// The cause is typically one of:
//   - A previous push happened via raw git, bypassing
//     gs's [state.IntegrationInfo.LastPushedHash] tracking.
//   - The same integration branch is being pushed from another
//     checkout (multi-checkout collision — inherently lossy).
//   - The spice state was reset (fresh clone, manual ref edit).
//
// Pulling is *not* the right resolution: the integration branch is a
// local throwaway with no durable upstream history.
type PushRejectedError struct {
	// Branch is the local integration branch name.
	Branch string

	// Remote is the configured push remote (e.g., "origin").
	Remote string

	// UpstreamBranch is the remote-side branch name.
	UpstreamBranch string

	// RemoteHash is the hash currently on the remote.
	RemoteHash git.Hash

	// LocalHash is the hash gs would have pushed.
	LocalHash git.Hash
}

func (e *PushRejectedError) Error() string {
	return fmt.Sprintf(
		"integration branch %q on %s is at %s; gs has no record of pushing it from this checkout",
		e.UpstreamBranch, e.Remote, e.RemoteHash.Short(),
	)
}

// Submit pushes the integration branch to the configured remote.
// Uses --force-with-lease against [state.IntegrationInfo.LastPushedHash]
// when available; otherwise pushes plainly.
//
// If the remote already has the branch and no LastPushedHash is
// recorded, returns [*PushRejectedError] without attempting the push.
// The user can reconcile state with [Handler.MarkPushed] and re-submit.
//
// No forge API is called; no PR is created.
func (h *Handler) Submit(ctx context.Context) error {
	info, err := h.loadConfigured(ctx)
	if err != nil {
		return err
	}

	remote, err := h.Store.Remote()
	if err != nil {
		return fmt.Errorf("get remote: %w", err)
	}
	pushRemote := remote.Push
	if pushRemote == "" {
		pushRemote = remote.Upstream
	}
	if pushRemote == "" {
		return errors.New("no push remote configured")
	}

	upstream := info.UpstreamBranch
	if upstream == "" {
		upstream = info.Name
	}

	currentHash, err := h.Repository.PeelToCommit(ctx, info.Name)
	if err != nil {
		return fmt.Errorf("resolve integration branch: %w", err)
	}

	// If there is no recorded LastPushedHash but the remote already has
	// this branch, a plain push would be rejected as non-fast-forward.
	// Detect this and surface a tailored error instead of letting git's
	// "use git pull" hint mislead the user.
	if info.LastPushedHash == "" {
		remoteHash, lookupErr := h.lookupRemoteRef(ctx, pushRemote, upstream)
		if lookupErr == nil && remoteHash != "" && remoteHash != currentHash {
			return &PushRejectedError{
				Branch:         info.Name,
				Remote:         pushRemote,
				UpstreamBranch: upstream,
				RemoteHash:     remoteHash,
				LocalHash:      currentHash,
			}
		}
	}

	opts := git.PushOptions{
		Remote:  pushRemote,
		Refspec: git.Refspec(info.Name + ":" + upstream),
	}
	if info.LastPushedHash != "" {
		opts.ForceWithLease = upstream + ":" + info.LastPushedHash.String()
	}

	if err := h.Worktree.Push(ctx, opts); err != nil {
		return fmt.Errorf("push: %w", err)
	}

	info.LastPushedHash = currentHash
	if err := h.Store.SetIntegration(ctx, info); err != nil {
		return fmt.Errorf("save integration state: %w", err)
	}
	return nil
}

// lookupRemoteRef returns the hash of refs/heads/<branch> on the named
// remote, or empty hash if the branch does not exist.
func (h *Handler) lookupRemoteRef(
	ctx context.Context, remote, branch string,
) (git.Hash, error) {
	for ref, err := range h.Repository.ListRemoteRefs(ctx, remote, &git.ListRemoteRefsOptions{
		Heads:    true,
		Patterns: []string{branch},
	}) {
		if err != nil {
			return "", err
		}
		if ref.Name == "refs/heads/"+branch {
			return ref.Hash, nil
		}
	}
	return "", nil
}

// MarkPushed records hash as the integration branch's last-pushed
// value. If hash is empty, auto-discovers it from the configured push
// remote.
//
// Used to reconcile gs state after a [PushRejectedError]: the user
// either trusts the remote and records that hash (then subsequent
// [Handler.Submit] uses --force-with-lease to overwrite), or
// investigates the divergence first.
func (h *Handler) MarkPushed(ctx context.Context, hash git.Hash) error {
	info, err := h.loadConfigured(ctx)
	if err != nil {
		return err
	}

	if hash == "" {
		remote, err := h.Store.Remote()
		if err != nil {
			return fmt.Errorf("get remote: %w", err)
		}
		pushRemote := remote.Push
		if pushRemote == "" {
			pushRemote = remote.Upstream
		}
		if pushRemote == "" {
			return errors.New("no push remote configured")
		}

		upstream := info.UpstreamBranch
		if upstream == "" {
			upstream = info.Name
		}

		remoteHash, err := h.lookupRemoteRef(ctx, pushRemote, upstream)
		if err != nil {
			return fmt.Errorf("lookup remote ref: %w", err)
		}
		if remoteHash == "" {
			return fmt.Errorf(
				"remote %s has no branch %q to mark as pushed",
				pushRemote, upstream)
		}
		hash = remoteHash
	}

	info.LastPushedHash = hash
	if err := h.Store.SetIntegration(ctx, info); err != nil {
		return fmt.Errorf("save integration state: %w", err)
	}
	return nil
}

// MaybeRebuild rebuilds the integration branch if any tracked tip's
// hash differs from the stored hash. No-op if not configured.
//
// A conflict during auto-rebuild is logged as a warning and does not
// return an error, since this is called from wrappers whose primary
// operation has already succeeded.
func (h *Handler) MaybeRebuild(ctx context.Context) error {
	info, err := h.Store.Integration(ctx)
	if errors.Is(err, state.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get integration: %w", err)
	}

	drifted, err := h.driftedTips(ctx, info)
	if err != nil {
		return err
	}
	if !drifted {
		return nil
	}

	h.Log.Infof("Rebuilding integration branch %q", info.Name)
	if _, err := h.Rebuild(ctx); err != nil {
		conflict := new(git.MergeConflictError)
		if errors.As(err, &conflict) {
			h.Log.Warnf("Integration rebuild failed: %v", err)
			return nil
		}
		return err
	}
	return nil
}

// MaybeRebuildAndSubmit invokes [MaybeRebuild] and, if the integration
// has previously been pushed (non-empty LastPushedHash), also submits.
//
// Used as a hook from gs stack/upstack submit. The first manual
// gs integration submit serves as the user's signal of intent to
// publish; afterward this hook keeps the branch in sync.
func (h *Handler) MaybeRebuildAndSubmit(ctx context.Context) error {
	if err := h.MaybeRebuild(ctx); err != nil {
		return err
	}

	info, err := h.Store.Integration(ctx)
	if errors.Is(err, state.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get integration: %w", err)
	}
	if info.LastPushedHash == "" {
		return nil
	}

	h.Log.Infof("Submitting integration branch %q", info.Name)
	if err := h.Submit(ctx); err != nil {
		h.Log.Warnf("Integration submit failed: %v", err)
		return nil
	}
	return nil
}

// loadConfigured returns the current integration config or
// [ErrNotConfigured].
func (h *Handler) loadConfigured(ctx context.Context) (*state.IntegrationInfo, error) {
	info, err := h.Store.Integration(ctx)
	if errors.Is(err, state.ErrNotExist) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, fmt.Errorf("get integration: %w", err)
	}
	return info, nil
}

// driftedTips reports whether any tip's current hash differs from its
// stored hash.
func (h *Handler) driftedTips(ctx context.Context, info *state.IntegrationInfo) (bool, error) {
	for _, tip := range info.Tips {
		hash, err := h.Repository.PeelToCommit(ctx, tip.Name)
		if err != nil {
			// A missing tip counts as drift; let Rebuild surface the
			// detailed error.
			return true, nil
		}
		if hash != tip.Hash {
			return true, nil
		}
	}
	return false, nil
}

// validateTipName ensures the proposed tip name is a tracked branch and
// distinct from trunk/integration/itself. seen is consulted for
// duplicate detection.
func (h *Handler) validateTipName(
	ctx context.Context,
	integrationName, tipName string,
	seen map[string]struct{},
) error {
	if tipName == "" {
		return errors.New("tip name is empty")
	}
	if tipName == h.Store.Trunk() {
		return errors.New("tip must not equal trunk")
	}
	if tipName == integrationName {
		return errors.New("tip must not equal integration branch name")
	}
	if _, dup := seen[tipName]; dup {
		return fmt.Errorf("duplicate tip %q", tipName)
	}
	if _, err := h.Service.LookupBranch(ctx, tipName); err != nil {
		return fmt.Errorf("tip %q is not tracked: %w", tipName, err)
	}
	return nil
}

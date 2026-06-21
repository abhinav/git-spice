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
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/spicedir"
	"go.abhg.dev/gs/internal/spice/state"
)

//go:generate mockgen -typed -destination mocks_test.go -package integration -write_package_comment=false . GitRepository,GitWorktree,Store,Service,Resolver,QuestionPrompter,Regenerator

// GitRepository is the subset of [git.Repository] used by the handler.
type GitRepository interface {
	PeelToCommit(ctx context.Context, ref string) (git.Hash, error)
	ListRemoteRefs(ctx context.Context, remote string, opts *git.ListRemoteRefsOptions) iter.Seq2[git.RemoteRef, error]
	Worktrees(ctx context.Context) iter.Seq2[*git.WorktreeListItem, error]
	GitDir() string
}

var _ GitRepository = (*git.Repository)(nil)

// GitWorktree is the subset of [git.Worktree] used by the handler.
type GitWorktree interface {
	RootDir() string
	CurrentBranch(ctx context.Context) (string, error)
	CheckoutBranch(ctx context.Context, branch string) error
	CheckoutNewBranch(ctx context.Context, req git.CheckoutNewBranchRequest) error
	CheckoutTheirs(ctx context.Context, paths []string) error
	Merge(ctx context.Context, opts git.MergeOptions) error
	MergeContinue(ctx context.Context, paths []string, message string) error
	AmendCommitAll(ctx context.Context) error
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

	// Resolver is invoked when a merge conflicts and auto-resolve is
	// enabled. nil means no resolver is configured; conflicts surface
	// normally.
	Resolver Resolver

	// Prompter collects user answers when the resolver returns
	// questions. nil disables the question-iteration loop (questions
	// become an immediate error).
	Prompter QuestionPrompter

	// DefaultAutoResolve sets the default behavior when
	// [RebuildOptions.AutoResolve] is nil. Typically populated from
	// spice.integration.autoResolve.
	DefaultAutoResolve bool

	// RepoRoot is the directory containing the resolution file.
	RepoRoot string

	// MaxResolveIterations bounds how many times the resolver may be
	// invoked for a single tip merge. Typically populated from
	// spice.scriptResolve.maxIterations via
	// Config.ScriptResolveMaxIterations. A non-positive value falls
	// back to the package default.
	MaxResolveIterations int

	// Regenerator, if non-nil, is invoked after all tip merges in a
	// rebuild succeed AND at least one path was logged via the
	// regenerate merge driver. It re-derives project-specific files
	// (mocks, CLI docs, test fixtures) that the take-incoming driver
	// could not merge correctly. nil disables the step entirely.
	Regenerator Regenerator

	// DefaultAcceptIncoming sets the default behavior when
	// [RebuildOptions.AcceptIncoming] is nil. Typically populated
	// from spice.integration.acceptIncoming.
	//
	// When true, conflicts that survive the merge drivers AND any
	// configured resolver are resolved by taking the incoming tip's
	// version, so the rebuild can complete without user intervention.
	// When false, surviving conflicts surface as a [ConflictError]
	// and the rebuild halts for manual resolution.
	DefaultAcceptIncoming bool
}

// defaultMaxResolveIterations is the fallback when
// Handler.MaxResolveIterations is non-positive. Matches
// DefaultScriptResolveMaxIterations in internal/spice.
const defaultMaxResolveIterations = 10

// resolveIterationCap returns the effective per-tip iteration cap.
func (h *Handler) resolveIterationCap() int {
	if h.MaxResolveIterations > 0 {
		return h.MaxResolveIterations
	}
	return defaultMaxResolveIterations
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

	// RegeneratorError is the error from the post-merge regenerator,
	// if one ran and failed. The rebuild itself succeeded (all tip
	// merges committed) and the partial regenerator output, if any,
	// was folded into the final merge commit — but the generated
	// files in the worktree may no longer match the merged source.
	// The CLI surfaces this so "rebuilt with N tips" does not paper
	// over a regen failure that leaves the integration build broken.
	RegeneratorError error
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

// ResolverFailedError indicates that the configured auto-resolver was
// invoked but produced corrupt or unusable output (e.g., script exit
// failure, malformed JSON, missing required fields). The merge is
// left in the worktree for the user to resolve manually, and pending
// rebuild state is saved so the rebuild can be resumed.
//
// Halting here — rather than falling through to accept-incoming — is
// deliberate. Modern LLMs reliably produce conforming output when
// prompted correctly; a corrupt response is a signal that the prompt,
// the model, or the resolver script itself needs attention. Silently
// accepting "theirs" instead would routinely drop integration-side
// API surface (methods, getters, fields) and bake the loss into a
// recorded rerere entry.
type ResolverFailedError struct {
	// Tip is the name of the tip whose merge invoked the resolver.
	Tip string

	// Paths are the files that were passed to the resolver.
	Paths []string

	// Cause is the underlying error returned by the resolver.
	Cause error
}

func (e *ResolverFailedError) Error() string {
	return fmt.Sprintf(
		"resolver failed for tip %q (%d conflicted file(s)): %v",
		e.Tip, len(e.Paths), e.Cause)
}

func (e *ResolverFailedError) Unwrap() error { return e.Cause }

// errResolverUnresolved is the sentinel returned by autoResolveLoop
// when the resolver responded with a valid structured "give up" —
// non-empty unresolved_files and no questions to ask. It is wrapped
// (via fmt.Errorf %w) so the caller can distinguish a structural
// surrender from a corrupt-output failure: the former drops to
// manual conflict resolution, the latter halts with a
// [*ResolverFailedError].
var errResolverUnresolved = errors.New("resolver reported unresolved files with no questions")

// RebuildOptions allows callers to override per-invocation behavior.
type RebuildOptions struct {
	// AutoResolve, if non-nil, overrides [Handler.DefaultAutoResolve]
	// for this invocation. A true value enables the configured
	// resolver; a false value disables it even when configured.
	AutoResolve *bool

	// AcceptIncoming, if non-nil, overrides
	// [Handler.DefaultAcceptIncoming] for this invocation. When true,
	// conflicts that remain after the resolver are auto-resolved by
	// taking the incoming tip's version.
	AcceptIncoming *bool

	// NoRerere, when true, disables rerere replay and recording for
	// this invocation. Use when a previous rebuild may have cached
	// a bad resolution that should not be replayed.
	NoRerere bool

	// ResetResolutionFile, when true, deletes the resolution file
	// (.integration_resolution.json) before starting the rebuild so
	// stale Q&A history is not carried into a fresh run.
	ResetResolutionFile bool

	// ResetPending, when true, clears any pending integration
	// rebuild state before starting so the rebuild starts from
	// trunk regardless of where a prior halted rebuild left off.
	ResetPending bool

	// ResetRerereCache, when true, deletes the rerere cache
	// directory before starting the rebuild. Distinct from
	// NoRerere: rerere stays enabled during the rebuild so the
	// fresh resolutions get recorded — the wipe just clears any
	// bad cached postimages from previous runs that would
	// otherwise be replayed silently.
	ResetRerereCache bool
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
//
// If a resolver is configured and auto-resolve is enabled (via opts
// or [Handler.DefaultAutoResolve]), Rebuild attempts to resolve
// conflicts automatically before surfacing them.
//
// A repo-scoped file lock at .spice_rebuild.lock ensures that two
// concurrent rebuild invocations (deliberate or otherwise — most
// often a user runs gs intrb in two shells before the first
// completes) cannot race on the worktree, the pending state, or
// the regen log. The second invocation fails fast with a clear
// message instead of corrupting state by interleaving with the
// first.
//
// opts may be nil to accept defaults.
func (h *Handler) Rebuild(
	ctx context.Context, opts *RebuildOptions,
) (*RebuildResult, error) {
	release, err := acquireRebuildLock(h.RepoRoot)
	if err != nil {
		return nil, err
	}
	defer release()

	if opts != nil && opts.ResetResolutionFile {
		if err := h.removeResolutionFile(); err != nil {
			return nil, fmt.Errorf("reset resolution file: %w", err)
		}
	}

	if opts != nil && opts.ResetPending {
		if err := h.Store.ClearPendingIntegrationRebuild(ctx); err != nil &&
			!errors.Is(err, state.ErrNotExist) {
			return nil, fmt.Errorf("reset pending rebuild: %w", err)
		}
	}

	if opts != nil && opts.ResetRerereCache {
		if err := h.resetRerereCache(); err != nil {
			return nil, fmt.Errorf("reset rerere cache: %w", err)
		}
	}

	info, err := h.loadConfigured(ctx)
	if err != nil {
		return nil, err
	}

	if err := h.ensureWorktreeSafe(ctx, info); err != nil {
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
		return h.resumeRebuild(ctx, info, pending, opts)
	}
	return h.freshRebuild(ctx, info, opts)
}

// ensureWorktreeSafe refuses to run an integration rebuild in a worktree
// it does not own.
//
// Rebuild force-checks-out the integration branch in the current worktree,
// merges the tips, and restores the original branch on completion. Doing
// that in a linked worktree that holds a tracked feature branch silently
// reverts that worktree's working tree to trunk content (observed as an
// AUTO_MERGE artifact plus a reset-to-HEAD in the worktree's gitdir).
//
// Two cases are rejected:
//
//   - The integration branch is checked out in a different worktree.
//     That worktree owns it; rebuilding here would either steal it
//     (failing opaquely inside git) or clobber it.
//   - We are in a multi-worktree repository and the current worktree has
//     a tracked feature branch checked out. Borrowing and restoring it
//     would revert that worktree. A single-worktree repository is always
//     safe to borrow, which is the normal interactive flow.
func (h *Handler) ensureWorktreeSafe(
	ctx context.Context,
	info *state.IntegrationInfo,
) error {
	currentWT := h.Worktree.RootDir()

	var integrationWT, currentBranch string
	var nonBare int
	for item, err := range h.Repository.Worktrees(ctx) {
		if err != nil {
			return fmt.Errorf("list worktrees: %w", err)
		}
		if item.Bare {
			continue
		}
		nonBare++
		if item.Branch == info.Name {
			integrationWT = item.Path
		}
		if item.Path == currentWT {
			currentBranch = item.Branch
		}
	}

	if integrationWT != "" && integrationWT != currentWT {
		return fmt.Errorf(
			"integration branch %q is checked out in another worktree (%s); "+
				"run the rebuild from there",
			info.Name, integrationWT)
	}

	// In a multi-worktree repo, refuse to hijack a worktree that has a
	// tracked feature branch checked out. An untracked or detached
	// checkout (currentBranch matched no branch / trunk) stays borrowable.
	if nonBare > 1 && integrationWT == "" &&
		currentBranch != "" &&
		currentBranch != info.Name &&
		currentBranch != h.Store.Trunk() {
		if _, err := h.Service.LookupBranch(ctx, currentBranch); err == nil {
			return fmt.Errorf(
				"refusing to rebuild integration branch %q here: "+
					"tracked branch %q is checked out in this worktree and "+
					"would be reverted; run the rebuild from the trunk or the "+
					"primary worktree, or check out %[1]q first",
				info.Name, currentBranch)
		}
	}

	return nil
}

// rebuildLockFileName is the well-known name of the rebuild lock
// file at the repository root.
const rebuildLockFileName = ".spice_rebuild.lock"

// acquireRebuildLock takes an exclusive on-disk lock at
// .spice_rebuild.lock relative to repoRoot. Returns a release
// function that the caller must defer.
//
// If the lock file already exists, returns an error naming the lock
// path and the PID recorded in the file (if any) so the user can
// either wait for the other rebuild to finish or remove the stale
// lock by hand. The lock is not crash-safe by design: it is better
// to require a manual cleanup after a hang than to risk two
// concurrent rebuilds clobbering each other.
func acquireRebuildLock(repoRoot string) (func(), error) {
	if repoRoot == "" {
		// Without a known root, just no-op the lock. Callers that
		// rely on it for safety (the CLI command path) always set
		// RepoRoot; tests that don't can opt out.
		return func() {}, nil
	}
	path := filepath.Join(repoRoot, rebuildLockFileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			holder := "unknown"
			if data, readErr := os.ReadFile(path); readErr == nil {
				holder = strings.TrimSpace(string(data))
			}
			return nil, fmt.Errorf(
				"another integration rebuild is in progress (held by pid %s); "+
					"if it has exited unexpectedly, remove %s and retry",
				holder, path)
		}
		return nil, fmt.Errorf("acquire rebuild lock: %w", err)
	}
	_, _ = fmt.Fprintln(file, os.Getpid())
	_ = file.Close()
	return func() {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			// Best-effort: a leftover lock blocks future rebuilds,
			// but we cannot do better here without panicking.
			_ = err
		}
	}, nil
}

// shouldAutoResolve resolves opts against DefaultAutoResolve.
func (h *Handler) shouldAutoResolve(opts *RebuildOptions) bool {
	if opts != nil && opts.AutoResolve != nil {
		return *opts.AutoResolve
	}
	return h.DefaultAutoResolve
}

// shouldAcceptIncoming resolves opts against DefaultAcceptIncoming.
func (h *Handler) shouldAcceptIncoming(opts *RebuildOptions) bool {
	if opts != nil && opts.AcceptIncoming != nil {
		return *opts.AcceptIncoming
	}
	return h.DefaultAcceptIncoming
}

func (h *Handler) freshRebuild(
	ctx context.Context,
	info *state.IntegrationInfo,
	opts *RebuildOptions,
) (*RebuildResult, error) {
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

	return h.runMerges(ctx, info, tips, 0, currentBranch, opts)
}

func (h *Handler) resumeRebuild(
	ctx context.Context,
	info *state.IntegrationInfo,
	pending *state.IntegrationRebuild,
	opts *RebuildOptions,
) (*RebuildResult, error) {
	// Pending state can advance past the last tip when an earlier
	// rebuild's auto-resolve loop committed the final merge but the
	// process was killed (or beaten to it by a concurrent run)
	// before the post-loop cleanup cleared the pending entry. In
	// that case there is genuinely nothing left to merge — just
	// clear the stale entry and report it explicitly so the user
	// doesn't see the meaningless "Resuming at tip N+1 of N" log.
	if pending.NextTipIndex >= len(pending.Tips) {
		h.Log.Infof(
			"Discarding pending rebuild state: all %d tip(s) already merged",
			len(pending.Tips))
		if err := h.Store.ClearPendingIntegrationRebuild(ctx); err != nil {
			h.Log.Warnf("clear pending rebuild: %v", err)
		}
		hashes := make([]git.Hash, len(pending.Tips))
		for i, t := range pending.Tips {
			hashes[i] = t.Hash
		}
		return &RebuildResult{Name: info.Name, TipHashes: hashes}, nil
	}

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
	return h.runMerges(ctx, info, pending.Tips, pending.NextTipIndex, currentBranch, opts)
}

// runMerges merges tips[start:] onto HEAD, finalizes the rebuild on
// success, and saves pending state + returns a [*ConflictError] on
// conflict (without aborting the merge).
//
// If auto-resolve is enabled for this invocation and a resolver is
// configured, conflicts are passed to the resolver before being
// surfaced. A successful resolve continues to the next tip; a failed
// one falls through to the original conflict-surfacing path.
func (h *Handler) runMerges(
	ctx context.Context,
	info *state.IntegrationInfo,
	tips []state.IntegrationTip,
	start int,
	originalBranch string,
	opts *RebuildOptions,
) (*RebuildResult, error) {
	// Accumulate paths whose conflicts the regenerate merge driver
	// resolved across all tip merges in this rebuild. After the loop
	// succeeds, gs hands the deduped list to the Regenerator (if any).
	var regenPaths []string

	enableRerere := opts == nil || !opts.NoRerere
	if !enableRerere {
		h.Log.Info("Rerere replay and recording disabled for this rebuild")
	}

	for i := start; i < len(tips); i++ {
		tip := tips[i]
		mergeMsg := fmt.Sprintf("Merge %s into %s", tip.Name, info.Name)

		// Set up a per-merge log file. The merge driver writes one
		// line per take-incoming resolution it performs.
		logFile, err := os.CreateTemp("", "gs-regen-log-*")
		if err != nil {
			return nil, fmt.Errorf("create regen log: %w", err)
		}
		logPath := logFile.Name()
		_ = logFile.Close()
		mergeEnv := []string{regenLogEnvVar + "=" + logPath}

		var rerereReplays []string
		onRerereReplay := func(path string) {
			rerereReplays = append(rerereReplays, path)
		}

		err = h.Worktree.Merge(ctx, git.MergeOptions{
			Refs:           []string{tip.Hash.String()},
			NoFF:           true,
			Message:        mergeMsg,
			EnableRerere:   enableRerere,
			LeaveConflict:  true,
			Env:            mergeEnv,
			OnRerereReplay: onRerereReplay,
		})

		if len(rerereReplays) > 0 {
			h.Log.Infof(
				"Tip %q: rerere replayed cached resolution for %d path(s): %s",
				tip.Name, len(rerereReplays),
				strings.Join(rerereReplays, ", "))
		}

		// Always drain + clean up the log file, regardless of merge
		// outcome.
		regenPaths = appendRegenLog(regenPaths, logPath)
		_ = os.Remove(logPath)

		if err == nil {
			continue
		}

		conflict := new(git.MergeConflictError)
		if !errors.As(err, &conflict) {
			return nil, fmt.Errorf("merge tip %q: %w", tip.Name, err)
		}

		// Try the auto-resolver if enabled and configured.
		if h.shouldAutoResolve(opts) && h.Resolver != nil {
			h.Log.Infof(
				"Tip %q: invoking resolver for %d conflicted path(s): %s",
				tip.Name, len(conflict.ConflictPaths),
				strings.Join(conflict.ConflictPaths, ", "))
			resolved, resolveErr := h.autoResolveLoop(
				ctx, info.Name, tip.Name, conflict.ConflictPaths, mergeMsg)
			switch {
			case resolveErr != nil:
				// Both branches save pending state and return so the
				// rebuild can be resumed after the user addresses the
				// underlying issue. Accept-incoming is bypassed in
				// both cases — silently picking "theirs" is exactly
				// the failure mode that motivated this whole flow.
				if saveErr := h.Store.SetPendingIntegrationRebuild(ctx, &state.IntegrationRebuild{
					Integration:  info.Name,
					Tips:         tips,
					NextTipIndex: i + 1,
				}); saveErr != nil {
					h.Log.Warnf("save pending rebuild: %v", saveErr)
				}

				// Structural "give up" from the resolver: valid JSON
				// with unresolved_files and no questions. The
				// resolver did its job; surface the conflict for
				// manual resolution.
				if errors.Is(resolveErr, errResolverUnresolved) {
					h.Log.Warnf(
						"Tip %q: resolver could not resolve: %v",
						tip.Name, resolveErr)
					return nil, &ConflictError{
						Tip:   tip.Name,
						Paths: conflict.ConflictPaths,
					}
				}

				// Corrupt or unusable resolver response (parse error,
				// non-zero exit, missing markers, iteration cap).
				// Halt with a distinct error so the caller knows the
				// prompt/model/script needs attention rather than
				// just a manual merge.
				return nil, &ResolverFailedError{
					Tip:   tip.Name,
					Paths: conflict.ConflictPaths,
					Cause: resolveErr,
				}
			case resolved:
				continue
			}
		}

		// Final fallback: accept the incoming tip's version of any
		// still-conflicted paths. This is the layer that lets a rebuild
		// complete without user intervention when no resolver script is
		// configured or the resolver could not resolve everything.
		if h.shouldAcceptIncoming(opts) && len(conflict.ConflictPaths) > 0 {
			if acceptErr := h.acceptIncoming(ctx, conflict.ConflictPaths, mergeMsg); acceptErr != nil {
				h.Log.Warnf("Accept-incoming failed for tip %q: %v",
					tip.Name, acceptErr)
				// fall through to conflict-surfacing
			} else {
				h.Log.Warnf(
					"Tip %q: accepted incoming for %d conflicted path(s) "+
						"(may have dropped declarations from the integration "+
						"side): %s",
					tip.Name, len(conflict.ConflictPaths),
					strings.Join(conflict.ConflictPaths, ", "))
				continue
			}
		}

		if saveErr := h.Store.SetPendingIntegrationRebuild(ctx, &state.IntegrationRebuild{
			Integration:  info.Name,
			Tips:         tips,
			NextTipIndex: i + 1,
		}); saveErr != nil {
			h.Log.Warnf("save pending rebuild: %v", saveErr)
		}
		return nil, &ConflictError{Tip: tip.Name, Paths: conflict.ConflictPaths}
	}

	// All tip merges succeeded. If any derived files were take-incoming
	// resolved AND a regenerator is configured, run it and fold any
	// resulting worktree changes into the last merge commit. AmendCommitAll
	// runs even if the regenerator exits non-zero: a partial run may have
	// written real updates that must not be left dirty in the worktree, and
	// `git commit --amend --no-edit --allow-empty` is a safe no-op when no
	// changes are present.
	//
	// regeneratorErr is preserved on [RebuildResult.RegeneratorError] so
	// the CLI surfaces it alongside the success summary; "rebuilt with N
	// tips" silently hiding a broken regen output is exactly what
	// confused users in earlier runs.
	var regeneratorErr error
	deduped := dedupStrings(regenPaths)
	if h.Regenerator != nil && len(deduped) > 0 {
		h.Log.Infof(
			"Invoking regenerator for %d take-incoming path(s): %s",
			len(deduped), strings.Join(deduped, ", "))
		if err := h.Regenerator.Regenerate(ctx, deduped); err != nil {
			regeneratorErr = err
			h.Log.Errorf(
				"Regenerator failed: %v. Integration branch still "+
					"contains the merged source, but its generated "+
					"files may be out of sync — your build will need "+
					"a manual 'mise run generate' (or your "+
					"project equivalent) before it compiles.",
				err)
		}
		if err := h.Worktree.AmendCommitAll(ctx); err != nil {
			return nil, fmt.Errorf("amend with regen output: %w", err)
		}
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
		Name:             info.Name,
		TipHashes:        hashes,
		RegeneratorError: regeneratorErr,
	}, nil
}

// regenLogEnvVar is the environment variable name the regenerate merge
// driver reads to find its append-only log file.
const regenLogEnvVar = "GS_INTEGRATION_REGEN_LOG"

// appendRegenLog reads each newline-separated path from the given log
// file (if any) and appends to dst. Missing or unreadable files are
// silently treated as empty.
func appendRegenLog(dst []string, logPath string) []string {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return dst
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			dst = append(dst, line)
		}
	}
	return dst
}

// dedupStrings returns ss without duplicates, preserving first
// occurrence order.
func dedupStrings(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
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
	if _, err := h.Rebuild(ctx, nil); err != nil {
		conflict := new(git.MergeConflictError)
		if errors.As(err, &conflict) {
			h.Log.Warnf("Integration rebuild failed: %v", err)
			return nil
		}
		return err
	}
	return nil
}

// acceptIncoming completes an in-progress merge by taking the incoming
// tip's version for every conflicted path and committing the merge.
// Used as a final, non-interactive fallback when no resolver is
// configured or the resolver leaves residual conflicts.
func (h *Handler) acceptIncoming(
	ctx context.Context, paths []string, mergeMsg string,
) error {
	if err := h.Worktree.CheckoutTheirs(ctx, paths); err != nil {
		return fmt.Errorf("checkout theirs: %w", err)
	}
	if err := h.Worktree.MergeContinue(ctx, paths, mergeMsg); err != nil {
		return fmt.Errorf("commit merge: %w", err)
	}
	return nil
}

// autoResolveLoop drives the resolver iteration for a single tip merge.
// Returns resolved=true if the merge was completed automatically.
//
// On resolver error, partial resolution, or iteration-cap hit, returns
// resolved=false along with an error describing the failure.
func (h *Handler) autoResolveLoop(
	ctx context.Context,
	integrationName, tipName string,
	conflictPaths []string,
	mergeMsg string,
) (resolved bool, err error) {
	req := &ResolveRequest{
		IntegrationName: integrationName,
		TipName:         tipName,
	}

	maxIters := h.resolveIterationCap()
	for iter := range maxIters {
		resp, resErr := h.Resolver.Resolve(ctx, req)
		if resErr != nil {
			return false, fmt.Errorf("resolver: %w", resErr)
		}

		for _, a := range resp.Assumptions {
			h.Log.Infof("Auto-resolve: %s", a)
		}

		if len(resp.Questions) > 0 {
			if h.Prompter == nil {
				return false, fmt.Errorf(
					"resolver returned %d question(s) but no prompter is configured",
					len(resp.Questions))
			}
			answers, askErr := h.Prompter.AskAnswers(ctx, resp.Questions)
			if askErr != nil {
				return false, fmt.Errorf("collect answers: %w", askErr)
			}
			if err := h.appendQAToFile(integrationName, tipName,
				resp.Questions, answers); err != nil {
				return false, fmt.Errorf("append Q&A: %w", err)
			}
			_ = iter
			continue
		}

		if len(resp.UnresolvedFiles) > 0 {
			return false, fmt.Errorf(
				"%w: %s", errResolverUnresolved,
				strings.Join(resp.UnresolvedFiles, ", "))
		}

		// All resolved. Commit the merge.
		if err := h.Worktree.MergeContinue(ctx, conflictPaths, mergeMsg); err != nil {
			return false, fmt.Errorf("commit merge: %w", err)
		}
		return true, nil
	}

	return false, fmt.Errorf(
		"resolver exceeded iteration cap (%d); investigate manually",
		maxIters)
}

// resetRerereCache deletes the rerere cache directory inside the
// git dir. Subsequent rerere recording during the rebuild repopulates
// it from scratch — that's the point: stale bad postimages from a
// prior run are gone, but rerere is still on so good resolutions
// from this rebuild get cached for the next one.
//
// Missing cache directory is a no-op (rerere may never have run).
func (h *Handler) resetRerereCache() error {
	path := filepath.Join(h.Repository.GitDir(), "rr-cache")
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	h.Log.Infof("Rerere cache cleared: %s", path)
	return nil
}

// removeResolutionFile deletes the resolution file under .spice/
// if one exists. Missing root or missing file are no-ops.
func (h *Handler) removeResolutionFile() error {
	if h.RepoRoot == "" {
		return nil
	}
	path := spicedir.ResolutionPath(h.RepoRoot, ResolutionFeatureName)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// appendQAToFile appends the given question/answer pairs to the
// resolution file's entry for (ours, theirs).
func (h *Handler) appendQAToFile(
	ours, theirs string, questions, answers []string,
) error {
	if err := spicedir.EnsureResolutionsDir(h.RepoRoot); err != nil {
		return err
	}
	path := spicedir.ResolutionPath(h.RepoRoot, ResolutionFeatureName)
	file, err := LoadResolutionFile(path)
	if err != nil {
		return err
	}

	pair := MergePair{Ours: ours, Theirs: theirs}
	qa := make([]scriptrun.QAPair, 0, len(questions))
	for i, q := range questions {
		a := ""
		if i < len(answers) {
			a = answers[i]
		}
		qa = append(qa, scriptrun.QAPair{Question: q, Answer: a})
	}
	file.AppendInstructions(pair, qa...)
	return file.Save(path)
}

// OnBranchRemoved prunes references to the removed branch:
//   - resolution-file entries that name it
//   - the integration's configured tip list, if it appears there
//
// Used as a hook from branch_untrack, branch_delete, and
// repo_sync's cleanup of merged branches. Skipping the tip-list
// prune would leave a dangling tip name in state, causing the
// next 'gs integration rebuild' to fail with "resolve head: does
// not exist" on a branch the user already chose to delete.
//
// Errors are returned; callers may choose to log them as warnings.
func (h *Handler) OnBranchRemoved(ctx context.Context, branch string) error {
	if err := h.pruneTipFromIntegration(ctx, branch); err != nil {
		return err
	}

	if h.RepoRoot == "" {
		// Cannot prune resolution file without a known root; treat as no-op.
		return nil
	}
	path := spicedir.ResolutionPath(h.RepoRoot, ResolutionFeatureName)

	file, err := LoadResolutionFile(path)
	if err != nil {
		return err
	}
	if file.PruneBranch(branch) == 0 {
		return nil
	}
	return file.Save(path)
}

// pruneTipFromIntegration removes branch from the integration's
// configured tip list if it appears there. No-op when no
// integration is configured, when the branch is not a tip, or
// when state is otherwise unreadable.
func (h *Handler) pruneTipFromIntegration(
	ctx context.Context, branch string,
) error {
	info, err := h.Store.Integration(ctx)
	if errors.Is(err, state.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get integration: %w", err)
	}

	idx := slices.IndexFunc(info.Tips, func(t state.IntegrationTip) bool {
		return t.Name == branch
	})
	if idx < 0 {
		return nil
	}

	info.Tips = slices.Delete(info.Tips, idx, idx+1)
	if err := h.Store.SetIntegration(ctx, info); err != nil {
		return fmt.Errorf("save integration: %w", err)
	}

	h.Log.Infof(
		"Removed %q from integration tips (branch was removed).",
		branch,
	)
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

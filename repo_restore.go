package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type repoRestoreCmd struct {
	Forget []string `name:"forget" placeholder:"PATH" help:"Discard a parked worktree whose branch is gone instead of restoring it (repeatable)"`
}

func (*repoRestoreCmd) Help() string {
	return text.Dedent(`
		Leaves exclusive mode: the worktrees recorded by 'gs repo park'
		are re-created at their branches' current tips, and the
		exclusive-mode marker is cleared.

		It is idempotent and resumable: worktrees that already exist are
		left alone, so an interrupted restore can be finished by
		re-running the command.

		If a parked branch no longer exists, the branch was deleted
		outside git-spice while the repository was parked. That leaves
		git-spice's state inconsistent, so restore cannot put the
		worktree back on its own. It restores every other worktree but
		stays in exclusive mode and reports what is wrong. Recover by
		either re-creating the missing branch and re-running restore, or
		discarding that worktree with --forget. The commit each worktree
		was parked at is preserved under refs/gs-park/ so it is not lost
		to garbage collection in the meantime.
	`)
}

func (cmd *repoRestoreCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	repo *git.Repository,
	store *state.Store,
) error {
	if !store.InExclusiveMode() {
		log.Infof("Repository is not in exclusive mode; nothing to restore")
		return nil
	}
	parked := store.ParkedWorktrees()

	forget := make(map[string]bool, len(cmd.Forget))
	for _, p := range cmd.Forget {
		match, ok := matchParked(parked, p)
		if !ok {
			return fmt.Errorf("--forget %q does not match any parked worktree", p)
		}
		forget[match] = true
	}

	// Worktrees that already exist on disk are skipped, so a re-run after
	// a crash only re-creates what is missing.
	existing := make(map[string]bool)
	for item, err := range repo.Worktrees(ctx) {
		if err != nil {
			return fmt.Errorf("list worktrees: %w", err)
		}
		existing[item.Path] = true
	}

	var (
		remaining []state.ParkedWorktree // manifest after this run
		blocked   []state.ParkedWorktree // parked branch is gone
	)
	for _, p := range parked {
		// Discard a worktree the user removed intentionally: drop it
		// from the manifest and release its pinned commit.
		if forget[p.Path] {
			releaseParkRef(ctx, repo, log, p.Head)
			log.Infof("Forgot parked worktree %s", p.Path)
			continue
		}
		remaining = append(remaining, p)

		if existing[p.Path] {
			continue
		}

		// A worktree parked on a branch that no longer exists cannot be
		// restored without guessing where its work went (the branch may
		// have been deleted, renamed, or rebased away). Don't fabricate
		// a branch at a possibly-stale commit; surface it instead.
		if p.Branch != "" && !repo.BranchExists(ctx, p.Branch) {
			blocked = append(blocked, p)
			continue
		}

		req := git.WorktreeAddRequest{Path: p.Path}
		if p.Branch != "" {
			req.Head = p.Branch
		} else {
			req.Detach, req.Head = true, p.Head
		}
		if err := repo.WorktreeAdd(ctx, req); err != nil {
			return fmt.Errorf("restore worktree %q: %w", p.Path, err)
		}
		log.Infof("Restored worktree %s", p.Path)
	}

	if len(blocked) > 0 {
		// Persist the --forget removals so they survive the wedge, then
		// stay in exclusive mode until the inconsistency is resolved.
		if len(forget) > 0 {
			if err := store.Park(ctx, remaining); err != nil {
				return fmt.Errorf("update manifest: %w", err)
			}
		}
		return reportBlocked(log, blocked)
	}

	// Everything is restored or forgotten: release the pinned commits
	// and leave exclusive mode.
	for _, p := range remaining {
		releaseParkRef(ctx, repo, log, p.Head)
	}
	if err := store.Unpark(ctx); err != nil {
		return fmt.Errorf("leave exclusive mode: %w", err)
	}
	log.Infof("Restored worktrees; exclusive mode cleared")
	return nil
}

// matchParked resolves a user-supplied --forget path to a parked
// worktree path, accepting either the exact recorded path or a cleaned /
// absolute form of it.
func matchParked(parked []state.ParkedWorktree, path string) (string, bool) {
	candidates := []string{filepath.Clean(path)}
	if abs, err := filepath.Abs(path); err == nil {
		candidates = append(candidates, abs)
	}
	for _, p := range parked {
		if slices.Contains(candidates, p.Path) {
			return p.Path, true
		}
	}
	return "", false
}

// releaseParkRef removes the pin that kept a parked commit reachable.
// Cleanup is best-effort: a leftover ref is harmless.
func releaseParkRef(
	ctx context.Context, repo *git.Repository, log *silog.Logger, head string,
) {
	if head == "" {
		return
	}
	if err := repo.DeleteRef(ctx, parkRef(head)); err != nil {
		log.Debugf("Could not release parked commit %s: %v", head, err)
	}
}

// reportBlocked explains worktrees that could not be restored because
// their branch is gone, and how to recover, then returns an error so the
// command exits non-zero with exclusive mode still set.
func reportBlocked(log *silog.Logger, blocked []state.ParkedWorktree) error {
	for _, p := range blocked {
		log.Errorf("worktree %s: parked branch %q no longer exists", p.Path, p.Branch)
		if p.Anchor != "" && p.Anchor == p.Branch {
			log.Errorf("  %q was this worktree's anchor; "+
				"its registration and any branches stacked on it now dangle",
				p.Branch)
		}
		if p.Head != "" {
			log.Errorf("  parked commit %s is preserved at %s", p.Head, parkRef(p.Head))
		}
	}
	log.Infof("To recover, re-create each missing branch and re-run " +
		"'gs repo restore', for example: git branch BRANCH COMMIT")
	log.Infof("Or, to discard a worktree you removed intentionally: " +
		"gs repo restore --forget PATH")
	return fmt.Errorf(
		"restore incomplete: %d worktree(s) blocked by missing branches",
		len(blocked))
}

package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type repoParkCmd struct {
	Force bool `help:"Park worktrees even if they have uncommitted changes (changes are discarded)"`
}

func (*repoParkCmd) Help() string {
	return text.Dedent(`
		Enters exclusive mode: the whole repository is handed to a single
		process so it can reorganize the stack without contending with
		worktrees owned by other processes.

		Every linked worktree is recorded in a durable manifest and its
		directory is removed; the branches themselves are left untouched,
		so the entire graph remains reachable from the primary checkout.
		Run 'gs repo restore' to leave exclusive mode and re-create the
		worktrees.

		A worktree with staged, unstaged, or untracked changes is refused
		unless --force is given, which discards those changes. Stashes are
		repository-global and are never discarded. The manifest is written
		before any worktree is removed, so an interrupted park can be
		resumed by re-running the command.
	`)
}

func (cmd *repoParkCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	repo *git.Repository,
	store *state.Store,
) error {
	current := wt.RootDir()

	// Enumerate the linked worktrees to remove: everything except the
	// invoking worktree and the bare repository.
	var live []*git.WorktreeListItem
	for item, err := range repo.Worktrees(ctx) {
		if err != nil {
			return fmt.Errorf("list worktrees: %w", err)
		}
		if item.Bare || item.Path == current {
			continue
		}
		live = append(live, item)
	}

	// Pre-flight: refuse before touching anything if any worktree has
	// uncommitted changes and --force was not given.
	if !cmd.Force {
		var dirty []string
		for _, item := range live {
			isDirty, err := worktreeDirty(ctx, repo, item.Path)
			if err != nil {
				return fmt.Errorf("check %q for changes: %w", item.Path, err)
			}
			if isDirty {
				dirty = append(dirty, item.Path)
			}
		}
		if len(dirty) > 0 {
			return fmt.Errorf("worktrees have uncommitted changes: %s; "+
				"commit them or use --force to discard",
				strings.Join(dirty, ", "))
		}
	}

	// Record every worktree (live plus any already recorded by an
	// interrupted park) BEFORE removing anything, so a crash loses
	// nothing.
	byPath := make(map[string]state.ParkedWorktree)
	for _, p := range store.ParkedWorktrees() {
		byPath[p.Path] = p
	}
	for _, item := range live {
		byPath[item.Path] = state.ParkedWorktree{
			Path:   item.Path,
			Branch: item.Branch,
			Head:   item.Head.String(),
			Anchor: anchorForWorktree(store, item.Path),
		}
	}

	manifest := make([]state.ParkedWorktree, 0, len(byPath))
	for _, p := range byPath {
		manifest = append(manifest, p)
	}
	slices.SortFunc(manifest, func(a, b state.ParkedWorktree) int {
		return strings.Compare(a.Path, b.Path)
	})
	if err := store.Park(ctx, manifest); err != nil {
		return fmt.Errorf("enter exclusive mode: %w", err)
	}

	// Pin each parked commit under refs/gs-park/ so it stays reachable
	// for recovery even if the exclusive command deletes the branch that
	// held it and Git later prunes the now-unreachable object. The ref
	// is removed by 'gs repo restore' once the worktree is recovered.
	for _, p := range manifest {
		if p.Head == "" {
			continue
		}
		if err := repo.SetRef(ctx, git.SetRefRequest{
			Ref:    parkRef(p.Head),
			Hash:   git.Hash(p.Head),
			Reason: "git-spice: pin parked commit",
		}); err != nil {
			log.Warnf("Could not pin parked commit %s: %v", p.Head, err)
		}
	}

	// Remove the worktree directories; refs are left untouched.
	for _, item := range live {
		if err := repo.WorktreeRemove(ctx, git.WorktreeRemoveRequest{
			Path:  item.Path,
			Force: cmd.Force,
		}); err != nil {
			return fmt.Errorf("remove worktree %q: %w", item.Path, err)
		}
		log.Infof("Parked worktree %s", item.Path)
	}

	log.Infof("Parked %d worktree(s); repository is in exclusive mode", len(live))
	return nil
}

// worktreeDirty reports whether the worktree at the given path has any
// uncommitted changes: staged, unstaged, or untracked.
func worktreeDirty(ctx context.Context, repo *git.Repository, path string) (bool, error) {
	wt, err := repo.OpenWorktree(ctx, path)
	if err != nil {
		return false, fmt.Errorf("open worktree: %w", err)
	}

	staged, err := wt.DiffIndex(ctx, "HEAD")
	if err != nil {
		return false, fmt.Errorf("diff index: %w", err)
	}
	if len(staged) > 0 {
		return true, nil
	}

	for _, err := range wt.DiffWork(ctx) {
		if err != nil {
			return false, fmt.Errorf("diff work tree: %w", err)
		}
		return true, nil
	}

	for _, err := range wt.ListUntrackedFiles(ctx) {
		if err != nil {
			return false, fmt.Errorf("list untracked files: %w", err)
		}
		return true, nil
	}

	return false, nil
}

// parkRef is the ref under which a parked commit is pinned so it stays
// reachable while the repository is in exclusive mode. It is keyed by the
// commit hash, so it is unique and needs no separate bookkeeping.
func parkRef(head string) string {
	return "refs/gs-park/" + head
}

// anchorForWorktree returns the anchor branch registered for the worktree
// at the given path, or an empty string if it owns no anchor.
func anchorForWorktree(store *state.Store, path string) string {
	for _, a := range store.Anchors() {
		if a.Worktree == path {
			return a.Branch
		}
	}
	return ""
}

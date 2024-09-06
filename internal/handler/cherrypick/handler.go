// Package cherrypick implements a handler for cherry-pick operations.
package cherrypick

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
	"sort"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/autostash"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
)

// RestackHandler is a subset of the restack.Handler interface.
type RestackHandler interface {
	RestackUpstack(ctx context.Context, branch string, opts *restack.UpstackOptions) error
}

var _ RestackHandler = (*restack.Handler)(nil)

// GitRepository is a subset of the git.Repository interface.
type GitRepository interface {
	MergeTree(ctx context.Context, req git.MergeTreeRequest) (git.Hash, error)
	CommitTree(ctx context.Context, req git.CommitTreeRequest) (git.Hash, error)
	PeelToCommit(ctx context.Context, rev string) (git.Hash, error)
	ReadCommit(ctx context.Context, commitish string) (*git.CommitObject, error)
	DiffTree(ctx context.Context, treeish1, treeish2 string) iter.Seq2[git.FileStatus, error]
}

var _ GitRepository = (*git.Repository)(nil)

// GitWorktree is a subset of the git.Worktree interface.
type GitWorktree interface {
	DiffIndex(ctx context.Context, treeish string) ([]git.FileStatus, error)
	DiffWork(ctx context.Context) iter.Seq2[git.FileStatus, error]
	ListUntrackedFiles(ctx context.Context) iter.Seq2[string, error]
	Reset(ctx context.Context, commit string, opts git.ResetOptions) error
}

var _ GitWorktree = (*git.Worktree)(nil)

// AutostashHandler is a subset of the autostash.Handler interface.
type AutostashHandler interface {
	BeginAutostash(ctx context.Context, opts *autostash.Options) (cleanup func(*error), err error)
}

var _ AutostashHandler = (*autostash.Handler)(nil)

// Handler implements the 'commit pick' operation.
type Handler struct {
	Log        *silog.Logger    // required
	Repository GitRepository    // required
	Worktree   GitWorktree      // required
	Restack    RestackHandler   // required
	Autostash  AutostashHandler // required
}

// Options holds options for fixing up a commit.
type Options struct {
	// SignCommits indicates whether Git is configured to sign commits.
	SignCommits bool `default:"false" hidden:"" config:"@commit.gpgsign"`
}

// Request holds parameters for fixing up a commit.
type Request struct {
	// Commit is the commit to cherry-pick.
	Commit git.Hash // required

	// Branch is the branch to cherry-pick onto.
	// The HEAD of this branch is used as the base for the cherry-pick.
	Branch string // required

	Options *Options // optional
}

// CherryPickCommit applies the staged changes to the given commit
// downstack from the current branch.
func (h *Handler) CherryPickCommit(ctx context.Context, req *Request) (retErr error) {
	req.Options = cmp.Or(req.Options, &Options{})

	head, err := h.Repository.PeelToCommit(ctx, req.Branch)
	if err != nil {
		return fmt.Errorf("resolve branch %q: %w", req.Branch, err)
	}

	// Like git cherry-pick, refuse to run if there are staged changes.
	if stagedFiles, err := h.Worktree.DiffIndex(ctx, head.String()); err != nil {
		return fmt.Errorf("check staged changes: %w", err)
	} else if len(stagedFiles) > 0 {
		var files []string
		for _, f := range stagedFiles {
			files = append(files, f.Path)
		}
		slices.Sort(files)

		h.Log.Error("Local changes would be overwritten by cherry-pick:")
		for _, f := range files {
			h.Log.Error("  " + f)
		}
		return errors.New("cannot cherry-pick with staged changes")
	}

	commit, err := h.Repository.ReadCommit(ctx, req.Commit.String())
	if err != nil {
		return fmt.Errorf("read commit %s: %w", req.Commit, err)
	}

	switch len(commit.Parents) {
	case 0:
		return fmt.Errorf("cannot cherry-pick root commit: %s", req.Commit)
	case 1:
		// ok
	default:
		return fmt.Errorf("cannot cherry-pick merge commit: %s", req.Commit)
	}

	parent := commit.Parents[0]

	// MergeTree will give us a version of HEAD
	// with the diff of MergeBase..Branch1 applied to it.
	mergedTree, err := h.Repository.MergeTree(ctx, git.MergeTreeRequest{
		MergeBase: parent.String(),
		Branch1:   commit.Tree.String(),
		Branch2:   head.String(),
	})
	if err != nil {
		var conflictErr *git.MergeTreeConflictError
		if !errors.As(err, &conflictErr) {
			return fmt.Errorf("merge trees: %w", err)
		}

		h.Log.Errorf("Cannot pick %v onto %v", req.Commit.Short(), head.Short())
		for _, detail := range conflictErr.Details {
			h.Log.Errorf("  %s", detail.Message)
		}
		h.Log.Error("Try cherry-picking with:")
		h.Log.Error("  git cherry-pick " + req.Commit.String())

		files := slices.Sorted(conflictErr.Filenames())
		return fmt.Errorf("merge conflict in files: %v", strings.Join(files, ", "))
	}

	// If there are unstaged changes or untracked files in the worktree
	// that overlap with files changed by the cherry-pick,
	// we cannot proceed as this may lose local changes.
	localChanges := make(map[string]struct{})
	for f, err := range h.Worktree.DiffWork(ctx) {
		if err != nil {
			return fmt.Errorf("diff working tree: %w", err)
		}
		localChanges[f.Path] = struct{}{}
	}
	for path, err := range h.Worktree.ListUntrackedFiles(ctx) {
		if err != nil {
			return fmt.Errorf("list untracked files: %w", err)
		}
		localChanges[path] = struct{}{}
	}

	if len(localChanges) > 0 {
		// Diff between HEAD and the prepared tree to see what files would change.
		var conflicts []string
		for f, err := range h.Repository.DiffTree(ctx, head.String(), mergedTree.String()) {
			if err != nil {
				return fmt.Errorf("diff prepared tree: %w", err)
			}
			if _, ok := localChanges[f.Path]; ok {
				conflicts = append(conflicts, f.Path)
			}
		}

		sort.Strings(conflicts)
		if len(conflicts) > 0 {
			h.Log.Error("Local changes would be overwritten by cherry-pick:")
			for _, f := range conflicts {
				h.Log.Error("  " + f)
			}
			return errors.New("cherry-pick would overwrite local changes")
		}
	}

	// Safe to proceed with these changes.
	newCommit, err := h.Repository.CommitTree(ctx, git.CommitTreeRequest{
		Tree:    mergedTree,
		Message: commit.Message(),
		Parents: []git.Hash{head},
		// git cherry-pick sets committer to the current user,
		// leaving the author intact. We'll do the same.
		Author:  &commit.Author,
		GPGSign: req.Options.SignCommits,
	})
	if err != nil {
		return fmt.Errorf("create commit: %w", err)
	}

	h.Log.Debug("cherry-pick commit created",
		"old", req.Commit,
		"new", newCommit)

	// Commit successful.
	// Stash any local changes,
	// reset the branch and the worktree to the new commit,
	// and restack upstack branches before unstashing.
	cleanup, err := h.Autostash.BeginAutostash(ctx, &autostash.Options{
		Message:   fmt.Sprintf("git-spice: autostash before commit pick %v", req.Commit.Short()),
		Branch:    req.Branch,
		ResetMode: autostash.ResetNone, // we do our own reset
	})
	if err != nil {
		return fmt.Errorf("autostash: %w", err)
	}
	defer cleanup(&retErr)

	if err := h.Worktree.Reset(ctx, newCommit.String(), git.ResetOptions{
		Mode: git.ResetHard,
	}); err != nil {
		return fmt.Errorf("reset index: %w", err)
	}

	h.Log.Infof("%v: cherry-picked %v: %v", req.Branch, req.Commit.Short(), commit.Subject)

	return h.Restack.RestackUpstack(ctx, req.Branch, &restack.UpstackOptions{
		SkipStart: true,
	})
}

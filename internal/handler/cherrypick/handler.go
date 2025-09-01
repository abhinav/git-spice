// Package cherrypick implements a handler for cherry-pick operations.
package cherrypick

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/git"
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
	SetRef(ctx context.Context, req git.SetRefRequest) error
}

var _ GitRepository = (*git.Repository)(nil)

// GitWorktree is a subset of the git.Worktree interface.
type GitWorktree interface {
	Checkout(ctx context.Context, branch string) error
}

var _ GitWorktree = (*git.Worktree)(nil)

// Handler implements the 'commit pick' operation.
type Handler struct {
	Log        *silog.Logger  // required
	Repository GitRepository  // required
	Worktree   GitWorktree    // required
	Restack    RestackHandler // required
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
func (h *Handler) CherryPickCommit(ctx context.Context, req *Request) error {
	req.Options = cmp.Or(req.Options, &Options{})

	head, err := h.Repository.PeelToCommit(ctx, req.Branch)
	if err != nil {
		return fmt.Errorf("resolve branch %q: %w", req.Branch, err)
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

	// instead of cherry-picking, we'll rely on git merge-tree
	// to apply the difference between the commit and its parent
	// onto the current HEAD.
	parent := commit.Parents[0]
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
		h.Log.Error("Try cherry-picking manually for now.")

		files := slices.Sorted(conflictErr.Filenames())
		return fmt.Errorf("merge conflict in files: %v", strings.Join(files, ", "))
	}

	// TODO:
	//
	// Perhaps, instead of commit-tree,
	// check out the merged tree into the working tree,
	// and 'git commit'.
	//
	// Would allow --edit/--no-edit.

	newCommit, err := h.Repository.CommitTree(ctx, git.CommitTreeRequest{
		Tree:      mergedTree,
		Message:   commit.Message(),
		Parents:   []git.Hash{head},
		Author:    &commit.Author,
		Committer: &commit.Committer,
		GPGSign:   req.Options.SignCommits,
	})
	if err != nil {
		return fmt.Errorf("create commit: %w", err)
	}

	h.Log.Debug("cherry-pick commit created",
		"old", req.Commit,
		"new", newCommit)

	// Commit successful, update the branch to point to it.
	if err := h.Repository.SetRef(ctx, git.SetRefRequest{
		Ref:     "refs/heads/" + req.Branch,
		Hash:    newCommit,
		OldHash: head,
		Reason:  fmt.Sprintf("cherry-pick %s", req.Commit),
	}); err != nil {
		return fmt.Errorf("update branch %q: %w", req.Branch, err)
	}

	h.Log.Infof("%v: cherry-picked %v: %v", req.Branch, req.Commit.Short(), commit.Subject)
	// TODO: This doesn't move working tree to new HEAD
	// TODO: This doesn't correctly handle changes in the working tree.

	return h.Restack.RestackUpstack(ctx, req.Branch, &restack.UpstackOptions{
		SkipStart: true,
	})
}

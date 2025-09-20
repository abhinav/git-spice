// Package fixup implements handlers for fixing up commits.
package fixup

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
)

//go:generate mockgen -package fixup -typed -destination mocks_test.go . RestackHandler,GitWorktree,GitRepository,Service

// RestackHandler is a subset of the restack.Handler interface.
type RestackHandler interface {
	RestackUpstack(ctx context.Context, branch string, opts *restack.UpstackOptions) error
}

var _ RestackHandler = (*restack.Handler)(nil)

// GitWorktree is a subset of the git.Worktree interface.
type GitWorktree interface {
	Head(ctx context.Context) (git.Hash, error)
	DiffIndex(ctx context.Context, treeish string) ([]git.FileStatus, error)
	WriteIndexTree(ctx context.Context) (git.Hash, error)
	Rebase(ctx context.Context, req git.RebaseRequest) (err error)
	Reset(ctx context.Context, commit string, opts git.ResetOptions) error
}

var _ GitWorktree = (*git.Worktree)(nil)

// GitRepository is a subset of the git.Repository interface.
type GitRepository interface {
	IsAncestor(ctx context.Context, ancestor, descendant git.Hash) bool
	MergeTree(ctx context.Context, req git.MergeTreeRequest) (git.Hash, error)
	CommitTree(ctx context.Context, req git.CommitTreeRequest) (git.Hash, error)
	PeelToCommit(ctx context.Context, rev string) (git.Hash, error)
	ReadCommit(ctx context.Context, commitish string) (*git.CommitObject, error)
	ListCommits(ctx context.Context, commits git.CommitRange) iter.Seq2[git.Hash, error]
}

var _ GitRepository = (*git.Repository)(nil)

// Service is a subset of the spice.Service interface.
type Service interface {
	Trunk() string
	BranchGraph(ctx context.Context, opts *spice.BranchGraphOptions) (*spice.BranchGraph, error)
	RebaseRescue(ctx context.Context, req spice.RebaseRescueRequest) error
}

var _ Service = (*spice.Service)(nil)

// Handler implements commit fixup operations.
type Handler struct {
	Log        *silog.Logger  // required
	Restack    RestackHandler // required
	Worktree   GitWorktree    // required
	Repository GitRepository  // required
	Service    Service        // required
}

// Options holds options for fixing up a commit.
type Options struct {
	// SignCommits indicates whether Git is configured to sign commits.
	SignCommits bool `default:"false" hidden:"" config:"@commit.gpgsign"`

	// TODO: -a/--all option to stage all changes?
}

// Request holds parameters for fixing up a commit.
type Request struct {
	// TargetHash is the commit to fixup with the staged changes.
	TargetHash git.Hash // required

	// TargetBranch is the branch that [TargetHash] belongs to.
	// If unset, we'll determine this automatically
	// by searching downstack branches.
	TargetBranch string // optional

	// HeadBranch is the current branch.
	HeadBranch string // required

	Options *Options // optional
}

// FixupCommit applies the staged changes to the given commit
// downstack from the current branch.
func (h *Handler) FixupCommit(ctx context.Context, req *Request) error {
	req.Options = cmp.Or(req.Options, &Options{})

	head, err := h.Worktree.Head(ctx)
	if err != nil {
		return fmt.Errorf("determine HEAD: %w", err)
	}

	// Target commit must be an ancestor of HEAD.
	if !h.Repository.IsAncestor(ctx, req.TargetHash, head) {
		h.Log.Errorf("Target commit (%v) is not reachable from HEAD (%v)", req.TargetHash, head)
		return errors.New("fixup commit must be an ancestor of HEAD")
	}

	// But it must be more recent than trunk.
	//
	// TODO:
	// Non-restack version of this command that works for detached HEAD
	// would also support fixing up commits that are already in trunk.
	if trunkHash, err := h.Repository.PeelToCommit(ctx, h.Service.Trunk()); err == nil {
		if h.Repository.IsAncestor(ctx, req.TargetHash, trunkHash) {
			h.Log.Errorf("Target commit (%v) is already in trunk (%v)", req.TargetHash, trunkHash)
			return errors.New("cannot fixup a commit that has been merged into trunk")
		}
	}

	// There must be something to commit.
	if diff, err := h.Worktree.DiffIndex(ctx, head.String()); err != nil {
		return fmt.Errorf("diff index: %w", err)
	} else if len(diff) == 0 {
		return errors.New("no changes staged for commit")
	}

	// If a branch name is not provided,
	// identify the branch that the commit belongs to
	// by searching downstack branches.
	if req.TargetBranch == "" {
		graph, err := h.Service.BranchGraph(ctx, nil)
		if err != nil {
			return fmt.Errorf("fetch branch graph: %w", err)
		}

		branch, err := h.findCommitBranch(ctx, req.HeadBranch, req.TargetHash, graph)
		if err != nil {
			h.Log.Error("Unable to identify commit branch", "error", err)
			return errors.New("try using the prompt to select a commit")
		}

		h.Log.Debug("Identified commit branch", "branch", branch)
		req.TargetBranch = branch
	}

	targetCommit, err := h.Repository.ReadCommit(ctx, req.TargetHash.String())
	if err != nil {
		return fmt.Errorf("read target commit: %w", err)
	}

	plannedTree, err := h.Worktree.WriteIndexTree(ctx)
	if err != nil {
		return fmt.Errorf("write staged changes to tree: %w", err)
	}

	mergedTree, err := h.Repository.MergeTree(ctx, git.MergeTreeRequest{
		Branch1:   plannedTree.String(),
		Branch2:   req.TargetHash.String(),
		MergeBase: head.String(),
	})
	if err != nil {
		var conflictErr *git.MergeTreeConflictError
		if !errors.As(err, &conflictErr) {
			return fmt.Errorf("merge staged changes with commit: %w", err)
		}

		h.Log.Errorf("Staged changes conflict with commit %s:", req.TargetHash.Short())
		for _, detail := range conflictErr.Details {
			h.Log.Errorf("  %s", detail.Message)
		}
		h.Log.Error("Try unstaging some changes and running the command again.")

		files := slices.Sorted(conflictErr.Filenames())
		return fmt.Errorf("merge conflict in files: %v", strings.Join(files, ", "))
	}

	newCommit, err := h.Repository.CommitTree(ctx, git.CommitTreeRequest{
		Tree:      mergedTree,
		Parents:   targetCommit.Parents,
		Message:   targetCommit.Message(),
		GPGSign:   req.Options.SignCommits,
		Author:    &targetCommit.Author,
		Committer: &targetCommit.Committer,
		// TODO: support -e/--edit?
		// TODO: does this run pre-commit hooks?
	})
	if err != nil {
		return fmt.Errorf("commit staged changes to target commit: %w", err)
	}

	// Clean up the working tree before rebasing.
	// We just committed the staged changes,
	// and any unstaged changes will have been autostashed by parent.
	if err := h.Worktree.Reset(ctx, "HEAD", git.ResetOptions{Mode: git.ResetHard}); err != nil {
		return fmt.Errorf("reset working tree to new commit: %w", err)
	}

	// TODO: for now we'll do this with a rebase.
	// With git-replay or similar, we could do this without a rebase.
	if err := h.Worktree.Rebase(ctx, git.RebaseRequest{
		Branch:   req.TargetBranch,
		Onto:     newCommit.String(),
		Upstream: req.TargetHash.String(),
	}); err != nil {
		// If the rebase is interrupted by a conflict,
		// after it's resolved, just restack the upstack.
		var rebaseErr *git.RebaseInterruptError
		if errors.As(err, &rebaseErr) {
			return h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
				Err:     rebaseErr,
				Command: []string{"upstack", "restack", "--skip-start"},
				Branch:  req.TargetBranch,
			})
		}

		return fmt.Errorf("rebase onto new commit: %w", err)
	}

	return h.Restack.RestackUpstack(ctx, req.TargetBranch, &restack.UpstackOptions{
		SkipStart: true,
	})
}

type branchGraph interface {
	Downstack(branch string) iter.Seq[string]
	Lookup(name string) (item spice.LoadBranchItem, ok bool)
}

var _ branchGraph = (*spice.BranchGraph)(nil)

// findCommitBranch searches through known branches' commit ranges
// to find one that contains the given commit.
func (h *Handler) findCommitBranch(
	ctx context.Context,
	headBranch string,
	wantCommit git.Hash,
	graph branchGraph,
) (string, error) {
	for branch := range graph.Downstack(headBranch) {
		item, ok := graph.Lookup(branch)
		if !ok {
			// This should never happen.
			// Skip it if it does.
			continue
		}

		h.Log.Debug("Searching branch for commit",
			"branch", branch, "range", item.BaseHash.Short()+".."+item.Head.Short())
		commitRange := git.CommitRangeFrom(item.Head).ExcludeFrom(item.BaseHash)
		for commit, err := range h.Repository.ListCommits(ctx, commitRange) {
			if err != nil {
				h.Log.Error("Error listing commits for branch. Skipping.",
					"branch", branch, "error", err)
				continue
			}

			if commit == wantCommit {
				return branch, nil
			}
		}
	}

	return "", fmt.Errorf("commit not found in any tracked branch: %s", wantCommit)
}

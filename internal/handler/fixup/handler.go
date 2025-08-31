// Package fixup implements handlers for fixing up commits.
package fixup

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"runtime"
	"sync"
	"sync/atomic"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
)

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
}

var _ GitWorktree = (*git.Worktree)(nil)

// GitRepository is a subset of the git.Repository interface.
type GitRepository interface {
	IsAncestor(ctx context.Context, ancestor, descendant git.Hash) bool
	MergeTree(ctx context.Context, req git.MergeTreeRequest) (git.Hash, error)
	CommitTree(ctx context.Context, req git.CommitTreeRequest) (git.Hash, error)
	ReadCommit(ctx context.Context, commitish string) (*git.CommitObject, error)
	ListCommits(ctx context.Context, commits git.CommitRange) iter.Seq2[git.Hash, error]
}

var _ GitRepository = (*git.Repository)(nil)

// Service is a subset of the spice.Service interface.
type Service interface {
	BranchGraph(ctx context.Context, opts *spice.BranchGraphOptions) (*spice.BranchGraph, error)
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
}

// Request holds parameters for fixing up a commit.
type Request struct {
	// Commit is the commit to fixup with the staged changes.
	Commit git.Hash // required

	// Branch is the branch that the commit belongs to.
	// If unset, we'll determine this automatically.
	//
	// Provide this if it's known in advance to avoid wasted work.
	Branch string

	Options *Options // optional
}

// FixupCommit applies the staged changes to the given commit
// downstack from the current branch.
func (h *Handler) FixupCommit(ctx context.Context, req *Request) error {
	req.Options = cmp.Or(req.Options, &Options{})

	// If a branch name is not provided,
	// identify the branch that the commit belongs to.
	//
	// TODO: this is pretty expensive.
	// Might be a good idea to allow a --branch flag.
	if req.Branch == "" {
		branch, err := h.findCommitBranch(ctx, req.Commit)
		if err != nil {
			return fmt.Errorf("find commit branch: %w", err)
		}

		h.Log.Debug("Identified commit branch", "branch", branch)
		req.Branch = branch
	}

	targetCommit, err := h.Repository.ReadCommit(ctx, req.Commit.String())
	if err != nil {
		return fmt.Errorf("read target commit: %w", err)
	}

	head, err := h.Worktree.Head(ctx)
	if err != nil {
		return fmt.Errorf("determine HEAD: %w", err)
	}

	if diff, err := h.Worktree.DiffIndex(ctx, head.String()); err != nil {
		return fmt.Errorf("diff index: %w", err)
	} else if len(diff) == 0 {
		return errors.New("no changes staged for commit")
	}

	plannedTree, err := h.Worktree.WriteIndexTree(ctx)
	if err != nil {
		return fmt.Errorf("write staged changes to tree: %w", err)
	}

	mergedTree, err := h.Repository.MergeTree(ctx, git.MergeTreeRequest{
		Branch1:   plannedTree.String(),
		Branch2:   req.Commit.String(),
		MergeBase: head.String(),
	})
	if err != nil {
		// TODO: figure out error message; this probably means conflict
		h.Log.Error("Merging staged changes into target commit failed", "error", err)
		return errors.New("staged changes cannot be applied to the target commit")
	}

	newCommit, err := h.Repository.CommitTree(ctx, git.CommitTreeRequest{
		Tree:      mergedTree,
		Parents:   targetCommit.Parents,
		Message:   targetCommit.Message(),
		GPGSign:   req.Options.SignCommits,
		Author:    &targetCommit.Author,
		Committer: &targetCommit.Committer,
		// TODO: edit?
	})
	if err != nil {
		return fmt.Errorf("commit staged changes to target commit: %w", err)
	}

	// TODO: for now we'll do this with a rebase.
	// With git-replay or similar, we could do this without a rebase.
	if err := h.Worktree.Rebase(ctx, git.RebaseRequest{
		Branch:    req.Branch,
		Onto:      newCommit.String(),
		Upstream:  req.Commit.String(),
		Autostash: true,
	}); err != nil {
		return fmt.Errorf("rebase onto new commit: %w", err)
	}

	// TODO: check out original branch after restack
	return h.Restack.RestackUpstack(ctx, req.Branch, &restack.UpstackOptions{
		SkipStart: true,
	})
}

// findCommitBranch searches through known branches' commit ranges
// to find one that contains the given commit.
func (h *Handler) findCommitBranch(ctx context.Context, commit git.Hash) (string, error) {
	graph, err := h.Service.BranchGraph(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("fetch branch graph: %w", err)
	}

	type workItem struct {
		Branch string
		Head   git.Hash
		Base   git.Hash
	}
	workc := make(chan workItem)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg     sync.WaitGroup
		found  atomic.Bool
		result string
	)
	wantCommit := commit
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for work := range workc {
				if found.Load() {
					return
				}

				commitRange := git.CommitRangeFrom(work.Head).ExcludeFrom(work.Base)
				for commit, err := range h.Repository.ListCommits(ctx, commitRange) {
					if err != nil {
						// Commands will be killed when
						// the context is cancelled.
						// No reason to log these.
						continue
					}

					if commit == wantCommit {
						if !found.Swap(true) {
							result = work.Branch
							cancel()
						}
					}
				}
			}
		})
	}

outer:
	for branch := range graph.All() {
		item := workItem{
			Branch: branch.Name,
			Head:   branch.Head,
			Base:   branch.BaseHash,
		}

		select {
		case workc <- item:

		case <-ctx.Done():
			// A worker already found the commit.
			break outer
		}
	}
	close(workc)
	wg.Wait()

	if result == "" {
		return "", fmt.Errorf("commit not found in any tracked branch: %s", commit)
	}

	return result, nil
}

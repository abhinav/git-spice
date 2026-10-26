// Package fixup implements handlers for fixing up commits.
package fixup

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gitedit"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
)

//go:generate mockgen -package fixup -typed -destination mocks_test.go . RestackHandler,GitWorktree,GitRepository,Service

// RestackHandler is a subset of the restack.Handler interface.
type RestackHandler interface {
	RestackUpstack(ctx context.Context, req *restack.UpstackRequest) error
}

var _ RestackHandler = (*restack.Handler)(nil)

// GitWorktree is a subset of the git.Worktree interface.
type GitWorktree interface {
	Head(ctx context.Context) (git.Hash, error)
	IndexFile(ctx context.Context) (string, error)
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

	// Methods required by gitedit.Repository.
	GitDir() string
	Var(ctx context.Context, name string) (string, error)
	DiffTreePatch(ctx context.Context, w io.Writer,
		treeish1, treeish2 string) error
	Stripspace(ctx context.Context, input io.Reader,
		output io.Writer, opts *git.StripspaceOptions) error
	HookRun(ctx context.Context, hook string,
		opts *git.HookRunOptions) error
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
	Log        *silog.Logger       // required
	Restack    RestackHandler      // required
	Signals    gitedit.SignalStack // required
	Worktree   GitWorktree         // required
	Repository GitRepository       // required
	Service    Service             // required
}

// Options holds options for fixing up a commit.
type Options struct {
	// SignCommits indicates whether Git is configured
	// to sign commits.
	SignCommits bool `default:"false" hidden:"" config:"@commit.gpgsign"`

	// Edit opens an editor to modify the commit message.
	Edit bool `short:"e" help:"Open an editor to modify the commit message."`

	// NoVerify allows a commit
	// with commit-msg hooks bypassed.
	NoVerify bool `help:"Bypass commit-msg hooks."`

	// CommentString is the comment prefix for editor messages.
	CommentString string `hidden:"" config:"@core.commentString" default:"#"`

	// CleanupMode is the commit message cleanup mode.
	CleanupMode string `hidden:"" config:"@commit.cleanup" default:"strip"`

	// CommitVerbose is the verbosity level
	// for the editor diff.
	CommitVerbose bool `hidden:"" config:"@commit.verbose" default:"false"`

	Restack spice.AutoRestackMode `negatable:"" default:"upstack" config:"commitFixup.restack" enum:"none,upstack" help:"Whether to restack upstack branches."`

	// TODO: -a/--all option to stage all changes?
}

// Request holds parameters for fixing up a commit.
type Request struct {
	// TargetHash is the commit to fixup with the staged changes.
	//
	// The caller must already have verified that this commit
	// is reachable from HEAD and has not already been merged into trunk.
	TargetHash git.Hash // required

	// TargetBranch is the branch that [TargetHash] belongs to.
	// If unset, we'll determine this automatically
	// by searching downstack branches.
	TargetBranch string // optional

	// HeadBranch is the current branch.
	//
	// The caller must already have verified that HEAD has staged changes
	// to apply to TargetHash.
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

	// Determine the commit message.
	// If --edit is specified, open the editor for the user.
	commitMessage := targetCommit.Message()
	// TODO: instead of holding message in memory, we could stream it.
	if req.Options.Edit {
		// TODO: inject editor
		editor := &gitedit.Editor{
			Repository:    h.Repository,
			Signals:       h.Signals,
			Log:           h.Log,
			CommentString: req.Options.CommentString,
			CleanupMode:   req.Options.CleanupMode,
			Verbose:       req.Options.CommitVerbose,
		}

		var parent git.Hash
		if len(targetCommit.Parents) > 0 {
			parent = targetCommit.Parents[0]
		}

		var edited strings.Builder
		err := editor.EditCommitMessage(
			ctx,
			strings.NewReader(commitMessage),
			&edited,
			&gitedit.EditCommitMessageOptions{
				Env:      h.editorEnv(ctx),
				NoVerify: req.Options.NoVerify,
				Commit:   req.TargetHash,
				Parent:   parent,
			},
		)
		if err != nil {
			return fmt.Errorf("edit commit message: %w", err)
		}
		commitMessage = strings.TrimRight(edited.String(), "\n")
	}

	newCommit, err := h.Repository.CommitTree(ctx, git.CommitTreeRequest{
		Tree:      mergedTree,
		Parents:   targetCommit.Parents,
		Message:   commitMessage,
		GPGSign:   req.Options.SignCommits,
		Author:    &targetCommit.Author,
		Committer: &targetCommit.Committer,
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
		if rebaseErr, ok := errors.AsType[*git.RebaseInterruptError](err); ok {
			return h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
				Err:     rebaseErr,
				Command: []string{"upstack", "restack", "--skip-start"},
				Branch:  req.TargetBranch,
			})
		}

		return fmt.Errorf("rebase onto new commit: %w", err)
	}

	if req.Options.Restack.None() {
		return nil
	}

	return h.Restack.RestackUpstack(ctx, &restack.UpstackRequest{
		Branch: req.TargetBranch,
		Options: &restack.UpstackOptions{
			SkipStart: true,
		},
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

func (h *Handler) editorEnv(ctx context.Context) []string {
	indexFile, err := h.Worktree.IndexFile(ctx)
	if err == nil {
		if _, err := os.Stat(indexFile); err == nil {
			return []string{"GIT_INDEX_FILE=" + indexFile}
		}
	}
	return nil
}

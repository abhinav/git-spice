// Package squash implements logic for squash commands.
package squash

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
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

//go:generate mockgen -package squash -destination mocks_test.go . GitRepository,GitWorktree,Store,Service,RestackHandler

// GitRepository provides treeless read/write access to the Git state.
type GitRepository interface {
	CommitMessageRange(ctx context.Context, start string, stop string) ([]git.CommitMessage, error)
	SetRef(ctx context.Context, req git.SetRefRequest) error
}

var _ GitRepository = (*git.Repository)(nil)

// GitWorktree provides worktree-specific operations.
type GitWorktree interface {
	DetachHead(ctx context.Context, commitish string) error
	Checkout(ctx context.Context, branch string) error
	Reset(ctx context.Context, commit string, opts git.ResetOptions) error
	Commit(ctx context.Context, req git.CommitRequest) error
	Head(ctx context.Context) (git.Hash, error)
}

var _ GitWorktree = (*git.Worktree)(nil)

// Store is the git-spice data store.
type Store interface {
	Trunk() string
}

var _ Store = (*state.Store)(nil)

// Service is a subset of spice.Service.
type Service interface {
	VerifyRestacked(ctx context.Context, name string) error
	LookupBranch(ctx context.Context, name string) (*spice.LookupBranchResponse, error)
}

var _ Service = (*spice.Service)(nil)

// RestackHandler provides methods to restack branches.
type RestackHandler interface {
	RestackUpstack(ctx context.Context, branch string, opts *restack.UpstackOptions) error
}

// Handler handles gs's squash commands.
type Handler struct {
	Log        *silog.Logger  // required
	Repository GitRepository  // required
	Worktree   GitWorktree    // required
	Store      Store          // required
	Service    Service        // required
	Restack    RestackHandler // required
}

// Options defines options for the SquashBranch method.
// These are exposed as flags in the CLI
type Options struct {
	NoVerify bool `help:"Bypass pre-commit and commit-msg hooks."`

	// git.commentString is the prefix for comments in commit messages.
	CommentPrefix string `hidden:"" config:"@core.commentString" default:"#"`

	Message string `short:"m" placeholder:"MSG" help:"Use the given message as the commit message."`
}

// SquashBranch squashes all commits in the given branch into a single commit.
func (h *Handler) SquashBranch(ctx context.Context, branchName string, opts *Options) error {
	opts = cmp.Or(opts, &Options{})

	if branchName == h.Store.Trunk() {
		return errors.New("cannot squash the trunk branch")
	}

	if err := h.Service.VerifyRestacked(ctx, branchName); err != nil {
		var restackErr *spice.BranchNeedsRestackError
		if errors.As(err, &restackErr) {
			return fmt.Errorf("branch %v needs to be restacked before it can be squashed", branchName)
		}
		return fmt.Errorf("verify restacked: %w", err)
	}

	branch, err := h.Service.LookupBranch(ctx, branchName)
	if err != nil {
		return fmt.Errorf("lookup branch %q: %w", branchName, err)
	}

	// If no message was specified,
	// combine the commit messages of all commits in the branch
	// to form the initial commit message for the squashed commit.
	var commitTemplate string
	if opts.Message == "" {
		commitMessages, err := h.Repository.CommitMessageRange(ctx, branch.Head.String(), branch.BaseHash.String())
		if err != nil {
			return fmt.Errorf("get commit messages: %w", err)
		}

		commitTemplate = commitMessageTemplate(opts.CommentPrefix, commitMessages)
	}

	// Detach the HEAD so that we don't mess with the current branch
	// until the operation is confirmed successful.
	if err := h.Worktree.DetachHead(ctx, branchName); err != nil {
		return fmt.Errorf("detach HEAD: %w", err)
	}
	var reattachedHead bool
	defer func() {
		// Reattach the HEAD to the original branch
		// if we return early before the operation is complete.
		if !reattachedHead {
			if cerr := h.Worktree.Checkout(ctx, branchName); cerr != nil {
				h.Log.Error("Could not check out original branch",
					"branch", branchName,
					"error", cerr)
				err = errors.Join(err, cerr)
			}
		}
	}()

	// Perform a soft reset to the base commit.
	// The working tree and index will remain unchanged,
	// so the contents of the head commit will be staged.
	if err := h.Worktree.Reset(ctx, branch.BaseHash.String(), git.ResetOptions{
		Mode: git.ResetSoft,
	}); err != nil {
		return fmt.Errorf("reset to base commit: %w", err)
	}

	if err := h.Worktree.Commit(ctx, git.CommitRequest{
		Message:  opts.Message,
		Template: commitTemplate,
		NoVerify: opts.NoVerify,
	}); err != nil {
		return fmt.Errorf("commit squashed changes: %w", err)
	}

	headHash, err := h.Worktree.Head(ctx)
	if err != nil {
		return fmt.Errorf("get HEAD hash: %w", err)
	}

	if err := h.Repository.SetRef(ctx, git.SetRefRequest{
		Ref:  "refs/heads/" + branchName,
		Hash: headHash,
		// Ensure that another tree didn't update the branch
		// while we weren't looking.
		OldHash: branch.Head,
	}); err != nil {
		return fmt.Errorf("update branch ref: %w", err)
	}

	if cerr := h.Worktree.Checkout(ctx, branchName); cerr != nil {
		return fmt.Errorf("checkout branch: %w", cerr)
	}
	reattachedHead = true

	return h.Restack.RestackUpstack(ctx, branchName, nil)
}

func commitMessageTemplate(commentPrefix string, commits []git.CommitMessage) string {
	commentPrefix = cmp.Or(commentPrefix, "#")
	var sb strings.Builder
	switch len(commits) {
	case 0:
		fmt.Fprintln(&sb, commentPrefix, "No commits to squash.")
	case 1:
		fmt.Fprintln(&sb, commits[0])
	default:
		// We want the earliest commit messages first.
		slices.Reverse(commits)
		fmt.Fprintf(&sb, "%s This is a combination of %d commits.\n", commentPrefix, len(commits))
		for i, msg := range commits {
			if i == 0 {
				fmt.Fprintf(&sb, "%s This is the 1st commit message:\n\n", commentPrefix)
			} else {
				fmt.Fprintf(&sb, "\n%s This is the commit message #%d:\n\n", commentPrefix, i+1)
			}
			fmt.Fprintln(&sb, msg)
		}
	}
	return sb.String()
}

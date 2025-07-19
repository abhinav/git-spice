package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchSquashCmd struct {
	NoVerify bool `help:"Bypass pre-commit and commit-msg hooks."`

	// git.commentString is the prefix for comments in commit messages.
	CommentPrefix string `hidden:"" config:"@core.commentString" default:"#"`

	Message string `short:"m" placeholder:"MSG" help:"Use the given message as the commit message."`
}

func (*branchSquashCmd) Help() string {
	return text.Dedent(`
		Squash all commits in the current branch into a single commit
		and restack upstack branches.

		An editor will open to edit the commit message of the squashed commit.
		Use the -m/--message flag to specify a commit message without editing.
	`)
}

func (cmd *branchSquashCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	restackHandler RestackHandler,
) (err error) {
	branchName, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	if branchName == store.Trunk() {
		return errors.New("cannot squash the trunk branch")
	}

	if err := svc.VerifyRestacked(ctx, branchName); err != nil {
		var restackErr *spice.BranchNeedsRestackError
		if errors.As(err, &restackErr) {
			return fmt.Errorf("branch %v needs to be restacked before it can be squashed", branchName)
		}
		return fmt.Errorf("verify restacked: %w", err)
	}

	branch, err := svc.LookupBranch(ctx, branchName)
	if err != nil {
		return fmt.Errorf("lookup branch %q: %w", branchName, err)
	}

	// If no message was specified,
	// combine the commit messages of all commits in the branch
	// to form the initial commit message for the squashed commit.
	var commitTemplate string
	if cmd.Message == "" {
		commitMessages, err := repo.CommitMessageRange(ctx, branch.Head.String(), branch.BaseHash.String())
		if err != nil {
			return fmt.Errorf("get commit messages: %w", err)
		}

		var sb strings.Builder
		switch len(commitMessages) {
		case 1:
			fmt.Fprintln(&sb, commitMessages[0])
		default:
			// We want the earliest commit messages first.
			slices.Reverse(commitMessages)

			fmt.Fprintf(&sb, "%s This is a combination of %d commits.\n", cmd.CommentPrefix, len(commitMessages))
			for i, msg := range commitMessages {
				if i == 0 {
					fmt.Fprintf(&sb, "%s This is the 1st commit message:\n\n", cmd.CommentPrefix)
				} else {
					fmt.Fprintf(&sb, "\n%s This is the commit message #%d:\n\n", cmd.CommentPrefix, i+1)
				}
				fmt.Fprintln(&sb, msg)
			}
		}

		commitTemplate = sb.String()
	}

	// Detach the HEAD so that we don't mess with the current branch
	// until the operation is confirmed successful.
	if err := wt.DetachHead(ctx, branchName); err != nil {
		return fmt.Errorf("detach HEAD: %w", err)
	}
	var reattachedHead bool
	defer func() {
		// Reattach the HEAD to the original branch
		// if we return early before the operation is complete.
		if !reattachedHead {
			if cerr := wt.Checkout(ctx, branchName); cerr != nil {
				log.Error("Could not check out original branch",
					"branch", branchName,
					"error", cerr)
				err = errors.Join(err, cerr)
			}
		}
	}()

	// Perform a soft reset to the base commit.
	// The working tree and index will remain unchanged,
	// so the contents of the head commit will be staged.
	if err := wt.Reset(ctx, branch.BaseHash.String(), git.ResetOptions{
		Mode: git.ResetSoft,
	}); err != nil {
		return fmt.Errorf("reset to base commit: %w", err)
	}

	if err := wt.Commit(ctx, git.CommitRequest{
		Message:  cmd.Message,
		Template: commitTemplate,
		NoVerify: cmd.NoVerify,
	}); err != nil {
		return fmt.Errorf("commit squashed changes: %w", err)
	}

	headHash, err := wt.Head(ctx)
	if err != nil {
		return fmt.Errorf("get HEAD hash: %w", err)
	}

	if err := repo.SetRef(ctx, git.SetRefRequest{
		Ref:  "refs/heads/" + branchName,
		Hash: headHash,
		// Ensure that another tree didn't update the branch
		// while we weren't looking.
		OldHash: branch.Head,
	}); err != nil {
		return fmt.Errorf("update branch ref: %w", err)
	}

	if cerr := wt.Checkout(ctx, branchName); cerr != nil {
		return fmt.Errorf("checkout branch: %w", cerr)
	}
	reattachedHead = true

	return restackHandler.RestackUpstack(ctx, branchName, nil)
}

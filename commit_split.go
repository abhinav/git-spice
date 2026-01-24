package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type commitSplitCmd struct {
	Message  string `short:"m" placeholder:"MSG" help:"Commit message for first split."`
	NoVerify bool   `help:"Bypass pre-commit and commit-msg hooks."`
	Commit   string `arg:"" optional:"" help:"Commit to split (default: HEAD)."`
}

func (*commitSplitCmd) Help() string {
	return text.Dedent(`
		Interactively select hunks from a commit
		to split into multiple new commits below it.
		Branches upstack are restacked as needed.

		The commit defaults to HEAD.
		If a different commit is specified,
		an interactive rebase will be used to split it.

		The split process loops until all changes are committed.
		Press 'q' during hunk selection to stop splitting
		and commit any remaining changes together.
	`)
}

func (cmd *commitSplitCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	svc *spice.Service,
	restackHandler RestackHandler,
) (err error) {
	// Determine which commit to split.
	targetCommit := cmd.Commit
	if targetCommit == "" {
		targetCommit = "HEAD"
	}

	target, err := repo.PeelToCommit(ctx, targetCommit)
	if err != nil {
		return fmt.Errorf("resolve commit %q: %w", targetCommit, err)
	}

	head, err := wt.Head(ctx)
	if err != nil {
		return fmt.Errorf("get HEAD: %w", err)
	}

	// Check if splitting HEAD or a different commit.
	if target == head {
		return cmd.splitHead(ctx, log, view, repo, wt, restackHandler)
	}

	// For non-HEAD commits, we need to use rebase.
	return cmd.splitNonHead(ctx, log, view, repo, wt, svc, restackHandler, target)
}

// splitHead handles splitting the HEAD commit.
func (cmd *commitSplitCmd) splitHead(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	restackHandler RestackHandler,
) (err error) {
	head, err := wt.Head(ctx)
	if err != nil {
		return fmt.Errorf("get HEAD: %w", err)
	}

	// Read original commit message for the final commit.
	commitObj, err := repo.ReadCommit(ctx, head.String())
	if err != nil {
		return fmt.Errorf("read commit: %w", err)
	}
	originalMessage := commitObj.Message()

	parent, err := repo.PeelToCommit(ctx, head.String()+"^")
	if err != nil {
		return fmt.Errorf("get HEAD^: %w", err)
	}

	// Reset to parent, keeping working tree.
	if err := wt.Reset(ctx, parent.String(), git.ResetOptions{
		Mode: git.ResetMixed,
	}); err != nil {
		return fmt.Errorf("reset to HEAD^: %w", err)
	}

	defer func() {
		if err != nil {
			ctx := context.WithoutCancel(ctx)
			log.Warn("Rolling back to previous commit", "commit", head)
			err = errors.Join(err, wt.Reset(ctx, head.String(), git.ResetOptions{
				Mode: git.ResetMixed,
			}))
		}
	}()

	// Run the multi-way split loop.
	if err := cmd.splitLoop(ctx, log, view, wt, head, parent, originalMessage); err != nil {
		return err
	}

	// Check if we're in the middle of a rebase.
	if _, err := wt.RebaseState(ctx); err == nil {
		log.Debug("A rebase is in progress, skipping restack")
		return nil
	}

	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		if errors.Is(err, git.ErrDetachedHead) {
			log.Debug("HEAD is detached, skipping restack")
			return nil
		}
		return fmt.Errorf("get current branch: %w", err)
	}

	return restackHandler.RestackUpstack(ctx, currentBranch, &restack.UpstackOptions{
		SkipStart: true,
	})
}

// splitNonHead handles splitting a commit that is not HEAD using rebase.
func (cmd *commitSplitCmd) splitNonHead(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	svc *spice.Service,
	restackHandler RestackHandler,
	target git.Hash,
) error {
	head, err := wt.Head(ctx)
	if err != nil {
		return fmt.Errorf("get HEAD: %w", err)
	}

	// Validate target is an ancestor of HEAD.
	if !repo.IsAncestor(ctx, target, head) {
		return fmt.Errorf("commit %v is not in the current branch history", target.Short())
	}

	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		if !errors.Is(err, git.ErrDetachedHead) {
			return fmt.Errorf("get current branch: %w", err)
		}
		currentBranch = ""
	}

	// Start interactive rebase with "edit" at target commit.
	if err := wt.RebaseEdit(ctx, target); err != nil {
		rebaseErr := new(git.RebaseInterruptError)
		if !errors.As(err, &rebaseErr) {
			return fmt.Errorf("start rebase: %w", err)
		}

		// Rebase was interrupted as expected (deliberate edit).
		if rebaseErr.Kind != git.RebaseInterruptDeliberate {
			return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
				Err:     err,
				Command: []string{"commit", "split", target.String()},
				Branch:  currentBranch,
				Message: "interrupted: split commit " + target.Short(),
			})
		}
	}

	// Now HEAD is at target commit. Split it.
	if err := cmd.splitHead(ctx, log, view, repo, wt, restackHandler); err != nil {
		// If split failed, abort the rebase.
		ctx := context.WithoutCancel(ctx)
		if abortErr := wt.RebaseAbort(ctx); abortErr != nil {
			log.Warn("Failed to abort rebase", "error", abortErr)
		}
		return err
	}

	// Continue the rebase to complete.
	if err := wt.RebaseContinue(ctx, nil); err != nil {
		rebaseErr := new(git.RebaseInterruptError)
		if errors.As(err, &rebaseErr) {
			return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
				Err:     err,
				Command: []string{"upstack", "restack"},
				Branch:  currentBranch,
				Message: "interrupted: split commit continuation",
			})
		}
		return fmt.Errorf("continue rebase: %w", err)
	}

	// Restack upstack branches.
	if currentBranch == "" {
		log.Debug("HEAD is detached, skipping restack")
		return nil
	}

	return restackHandler.RestackUpstack(ctx, currentBranch, &restack.UpstackOptions{
		SkipStart: true,
	})
}

// splitLoop performs the multi-way split.
// It loops, allowing the user to select hunks for each split commit,
// until no more changes remain.
func (cmd *commitSplitCmd) splitLoop(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	wt *git.Worktree,
	original git.Hash,
	parent git.Hash,
	originalMessage string,
) error {
	commitNum := 1
	nextMessage := cmd.Message

	for {
		log.Infof("Select hunks for commit %d", commitNum)

		// Use git reset --patch to select hunks.
		// Can't use 'git add' here because reset will have unstaged
		// new files, which 'git add' will ignore.
		if err := wt.Reset(ctx, original.String(), git.ResetOptions{Patch: true}); err != nil {
			return fmt.Errorf("select hunks: %w", err)
		}

		// Check if anything was staged.
		staged, err := wt.DiffIndex(ctx, parent.String())
		if err != nil {
			return fmt.Errorf("check staged: %w", err)
		}
		if len(staged) == 0 {
			// User quit without selecting anything.
			log.Info("No hunks selected, stopping split")
			break
		}

		// Prompt for commit message if not provided.
		message := nextMessage
		if message == "" {
			if err := cmd.promptMessage(view, &message, originalMessage, commitNum); err != nil {
				return fmt.Errorf("prompt message: %w", err)
			}
		}
		nextMessage = "" // Only use -m for first commit.

		// Create the split commit.
		if err := wt.Commit(ctx, git.CommitRequest{
			Message:  message,
			NoVerify: cmd.NoVerify,
		}); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		// Update parent to the new commit for the next iteration.
		newHead, err := wt.Head(ctx)
		if err != nil {
			return fmt.Errorf("get new HEAD: %w", err)
		}
		parent = newHead

		// Reset index to remaining changes from original.
		if err := wt.Reset(ctx, original.String(), git.ResetOptions{
			Paths: []string{"."},
		}); err != nil {
			return fmt.Errorf("reset index: %w", err)
		}

		// Check if there are remaining changes.
		var hasRemaining bool
		for _, err := range wt.DiffWork(ctx) {
			if err != nil {
				return fmt.Errorf("check remaining: %w", err)
			}
			hasRemaining = true
			break
		}
		if !hasRemaining {
			// No more changes to split.
			log.Info("All changes committed")
			return nil
		}

		commitNum++
	}

	// Commit any remaining changes with original message.
	if err := wt.Reset(ctx, original.String(), git.ResetOptions{
		Paths: []string{"."},
	}); err != nil {
		return fmt.Errorf("reset index: %w", err)
	}

	staged, err := wt.DiffIndex(ctx, parent.String())
	if err != nil {
		return fmt.Errorf("check staged: %w", err)
	}
	if len(staged) > 0 {
		if err := wt.Commit(ctx, git.CommitRequest{
			ReuseMessage: original.String(),
			NoVerify:     cmd.NoVerify,
		}); err != nil {
			return fmt.Errorf("commit remainder: %w", err)
		}
	}

	return nil
}

// promptMessage prompts the user for a commit message.
func (cmd *commitSplitCmd) promptMessage(
	view ui.View,
	message *string,
	originalMessage string,
	commitNum int,
) error {
	if !ui.Interactive(view) {
		// Non-interactive mode: use a default message.
		*message = fmt.Sprintf("Split commit %d", commitNum)
		return nil
	}

	*message = originalMessage
	field := ui.NewInput().
		WithTitle(fmt.Sprintf("Commit %d message", commitNum)).
		WithDescription("Enter message for this split commit").
		WithValue(message).
		WithOptions([]string{originalMessage})

	return ui.Run(view, field)
}

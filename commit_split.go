package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type commitSplitCmd struct {
	Message  string `short:"m" placeholder:"MSG" help:"Use the given message as the commit message."`
	NoVerify bool   `help:"Bypass pre-commit and commit-msg hooks."`
}

func (*commitSplitCmd) Help() string {
	return text.Dedent(`
		Interactively select hunks from the current commit
		to split into new commits below it.
		Branches upstack are restacked as needed.
	`)
}

func (cmd *commitSplitCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	repo *git.Repository,
	wt *git.Worktree,
	restackHandler RestackHandler,
) (err error) {
	head, err := wt.Head(ctx)
	if err != nil {
		return fmt.Errorf("get HEAD: %w", err)
	}

	parent, err := repo.PeelToCommit(ctx, head.String()+"^")
	if err != nil {
		return fmt.Errorf("get HEAD^: %w", err)
	}

	if err := wt.Reset(ctx, parent.String(), git.ResetOptions{
		Mode: git.ResetMixed, // don't touch the working tree
	}); err != nil {
		return fmt.Errorf("reset to HEAD^: %w", err)
	}

	defer func() {
		if err != nil {
			// The operation may have failed
			// because the user pressed Ctrl-C.
			// That would invalidate the current context.
			// Create an uncanceled context to perform the rollback.
			ctx := context.WithoutCancel(ctx)

			log.Warn("Rolling back to previous commit", "commit", head)
			err = errors.Join(err, wt.Reset(ctx, head.String(), git.ResetOptions{
				Mode: git.ResetMixed,
			}))
		}
	}()

	log.Info("Select hunks to extract into a new commit")
	// Can't use 'git add' here because reset will have unstaged
	// new files, which 'git add' will ignore.
	if err := wt.Reset(ctx, head.String(), git.ResetOptions{Patch: true}); err != nil {
		return fmt.Errorf("select hunks: %w", err)
	}

	if err := wt.Commit(ctx, git.CommitRequest{
		Message:  cmd.Message,
		NoVerify: cmd.NoVerify,
	}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	if err := wt.Reset(ctx, head.String(), git.ResetOptions{
		Paths: []string{"."}, // reset index to remaining changes
	}); err != nil {
		return fmt.Errorf("select hunks: %w", err)
	}

	// Commit will move HEAD to the new commit,
	// updating branch ref if necessary.
	if err := wt.Commit(ctx, git.CommitRequest{
		ReuseMessage: head.String(),
		NoVerify:     cmd.NoVerify,
	}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	if _, err := wt.RebaseState(ctx); err == nil {
		// In the middle of a rebase.
		// Don't restack upstack branches.
		log.Debug("A rebase is in progress, skipping restack")
		return nil
	}

	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		// No restack needed if we're in a detached head state.
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

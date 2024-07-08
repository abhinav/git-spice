package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/text"
)

type commitSplitCmd struct {
	Message string `short:"m" placeholder:"MSG" help:"Use the given message as the commit message."`
}

func (*commitSplitCmd) Help() string {
	return text.Dedent(`
		Interactively select hunks from the current commit
		to split into new commits below it.
		Branches upstack are restacked as needed.
	`)
}

func (cmd *commitSplitCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) (err error) {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	head, err := repo.Head(ctx)
	if err != nil {
		return fmt.Errorf("get HEAD: %w", err)
	}

	parent, err := repo.PeelToCommit(ctx, head.String()+"^")
	if err != nil {
		return fmt.Errorf("get HEAD^: %w", err)
	}

	if err := repo.Reset(ctx, parent.String(), git.ResetOptions{
		Mode: git.ResetMixed, // don't touch the working tree
	}); err != nil {
		return fmt.Errorf("reset to HEAD^: %w", err)
	}

	defer func() {
		if err != nil {
			log.Warn("rolling back to previous commit", "commit", head)
			err = errors.Join(err, repo.Reset(ctx, head.String(), git.ResetOptions{
				Mode: git.ResetMixed,
			}))
		}
	}()

	log.Info("Select hunks to extract into a new commit")
	// Can't use 'git add' here because reset will have unstaged
	// new files, which 'git add' will ignore.
	if err := repo.Reset(ctx, head.String(), git.ResetOptions{Patch: true}); err != nil {
		return fmt.Errorf("select hunks: %w", err)
	}

	if err := repo.Commit(ctx, git.CommitRequest{Message: cmd.Message}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	if err := repo.Reset(ctx, head.String(), git.ResetOptions{
		Paths: []string{"."}, // reset index to remaining changes
	}); err != nil {
		return fmt.Errorf("select hunks: %w", err)
	}

	// Commit will move HEAD to the new commit,
	// updating branch ref if necessary.
	if err := repo.Commit(ctx, git.CommitRequest{ReuseMessage: head.String()}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	if _, err := repo.RebaseState(ctx); err == nil {
		// In the middle of a rebase.
		// Don't restack upstack branches.
		return nil
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		// No restack needed if we're in a detached head state.
		if errors.Is(err, git.ErrDetachedHead) {
			return nil
		}
		return fmt.Errorf("get current branch: %w", err)
	}

	return (&upstackRestackCmd{
		Branch:    currentBranch,
		SkipStart: true,
	}).Run(ctx, log, opts)
}

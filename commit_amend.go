package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type commitAmendCmd struct {
	All        bool   `short:"a" help:"Stage all changes before committing."`
	AllowEmpty bool   `help:"Create a commit even if it contains no changes."`
	Message    string `short:"m" placeholder:"MSG" help:"Use the given message as the commit message."`

	NoEdit   bool `help:"Don't edit the commit message"`
	NoVerify bool `help:"Bypass pre-commit and commit-msg hooks."`

	// TODO:
	// Remove this short form and put it on NoVerify.
	NoEditDeprecated bool `hidden:"true" short:"n" help:"Don't edit the commit message"`
}

func (*commitAmendCmd) Help() string {
	return text.Dedent(`
		Staged changes are amended into the topmost commit.
		Branches upstack are restacked if necessary.
		Use this as a shortcut for 'git commit --amend'
		followed by 'gs upstack restack'.
	`)
}

func (cmd *commitAmendCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
) error {
	if cmd.NoEditDeprecated {
		cmd.NoEdit = true
		log.Warn("flag '-n' is deprecated; use '--no-edit' instead")
	}

	if err := wt.Commit(ctx, git.CommitRequest{
		Message:    cmd.Message,
		AllowEmpty: cmd.AllowEmpty,
		Amend:      true,
		NoEdit:     cmd.NoEdit,
		NoVerify:   cmd.NoVerify,
		All:        cmd.All,
	}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

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

	return (&upstackRestackCmd{
		Branch:    currentBranch,
		SkipStart: true,
	}).Run(ctx, log, wt, store, svc)
}

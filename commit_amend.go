package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type commitAmendCmd struct {
	branchCreateConfig // TODO: find a way to avoid this

	All        bool   `short:"a" help:"Stage all changes before committing."`
	AllowEmpty bool   `help:"Create a commit even if it contains no changes."`
	Message    string `short:"m" placeholder:"MSG" help:"Use the given message as the commit message."`

	NoEdit   bool `help:"Don't edit the commit message"`
	NoVerify bool `help:"Bypass pre-commit and commit-msg hooks."`
}

func (*commitAmendCmd) Help() string {
	return text.Dedent(`
		Staged changes are amended into the topmost commit.
		Branches upstack are restacked if necessary.
		Use this as a shortcut for 'git commit --amend'
		followed by 'gs upstack restack'.

		An editor is opened to edit the commit message,
		unless the --no-edit flag is given.
		Use the -m/--message option to specify the message
		on the command line.
		Git hooks are run unless the --no-verify flag is given.

		Use the -a/--all flag to stage all changes before committing.

		To prevent accidental amends on the trunk branch,
		a prompt will require confirmation when amending on trunk.
		The --no-prompt flag can be used to skip this prompt in scripts.
	`)
}

func (cmd *commitAmendCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	restackHandler RestackHandler,
) error {
	var detachedHead bool
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		if !errors.Is(err, git.ErrDetachedHead) {
			return fmt.Errorf("get current branch: %w", err)
		}
		detachedHead = true
		currentBranch = ""
	}

	if currentBranch == store.Trunk() {
		if !ui.Interactive(view) {
			log.Warnf("You are about to amend a commit on the trunk branch (%v).", store.Trunk())
		} else {
			var (
				amendOnTrunk bool
				branchName   string
			)
			fields := []ui.Field{
				ui.NewList[bool]().
					WithTitle("Do you want to amend a commit on trunk?").
					WithDescription(fmt.Sprintf("You are about to amend a commit on the trunk branch (%v). "+
						"This is usually not what you want to do.", store.Trunk())).
					WithItems(
						ui.ListItem[bool]{
							Title: "Yes",
							Description: func(bool) string {
								return "Amend the commit on trunk"
							},
							Value: true,
						},
						ui.ListItem[bool]{
							Title: "No",
							Description: func(bool) string {
								return "Create a branch and commit there instead"
							},
							Value: false,
						},
					).
					WithValue(&amendOnTrunk),
				ui.Defer(func() ui.Field {
					if amendOnTrunk {
						return nil
					}

					return ui.NewInput().
						WithTitle("Branch name").
						WithDescription("What do you want to call the new branch?").
						WithValue(&branchName)
				}),
			}
			if err := ui.Run(view, fields...); err != nil {
				return fmt.Errorf("run prompt: %w", err)
			}
			if !amendOnTrunk {
				// TODO: shared commitOptions struct?
				return (&branchCreateCmd{
					branchCreateConfig: cmd.branchCreateConfig,
					Name:               branchName,
					All:                cmd.All,
					NoVerify:           cmd.NoVerify,
					Message:            cmd.Message,
					Commit:             true,
				}).Run(ctx, log, repo, wt, store, svc, restackHandler)
			}
		}
	}

	// Check if we're in the middle of a rebase with unmerged paths
	var rebasing bool
	if _, err := wt.RebaseState(ctx); err == nil {
		rebasing = true

		// If we're in the middle of a rebase,
		// and there are unmerged paths,
		// what the user likely wants is 'git add' and 'gs rebase continue'.
		//
		// (If there are no unmerged paths, amending is fine.)
		var numUnmerged int
		for _, err := range wt.ListFilesPaths(ctx, &git.ListFilesOptions{Unmerged: true}) {
			if err == nil {
				numUnmerged++
			}
		}

		if numUnmerged > 0 {
			if !ui.Interactive(view) {
				log.Warnf("You are in the middle of a rebase with unmerged paths.")
				log.Warnf(`You probably want resolve the conflicts and run "git add", then "gs rebase continue" instead.`)
			} else {
				var continueAmend bool
				fields := []ui.Field{
					ui.NewList[bool]().
						WithTitle("Do you want to amend the commit?").
						WithDescription("You are in the middle of a rebase with unmerged paths.\n"+
							"You might want to resolve the conflicts and run 'git add', then 'gs rebase continue' instead.").
						WithItems(
							ui.ListItem[bool]{
								Title:       "Yes",
								Description: func(bool) string { return "Continue with commit amend" },
								Value:       true,
							},
							ui.ListItem[bool]{
								Title:       "No",
								Description: func(bool) string { return "Abort the operation" },
								Value:       false,
							},
						).
						WithValue(&continueAmend),
				}
				if err := ui.Run(view, fields...); err != nil {
					return fmt.Errorf("run prompt: %w", err)
				}
				if !continueAmend {
					return errors.New("operation aborted")
				}
			}
		}
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

	if rebasing {
		log.Debug("A rebase is in progress, skipping restack")
		return nil
	}

	if detachedHead {
		log.Debug("HEAD is detached, skipping restack")
		return nil
	}

	return restackHandler.RestackUpstack(ctx, currentBranch, &restack.UpstackOptions{
		SkipStart: true,
	})
}

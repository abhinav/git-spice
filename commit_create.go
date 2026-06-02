package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/handler/submodule"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type commitCreateCmd struct {
	All           bool              `short:"a" help:"Stage all changes before committing."`
	AllowEmpty    bool              `help:"Create a new commit even if it contains no changes."`
	Fixup         string            `help:"Create a fixup commit. See also 'git-spice commit fixup'." placeholder:"COMMIT"`
	Message       string            `short:"m" xor:"commit-message-source" placeholder:"MSG" help:"Use the given message as the commit message."`
	MessageFile   string            `short:"F" xor:"commit-message-source" placeholder:"FILE" help:"Read the commit message from the given file."`
	NoVerify      bool              `help:"Bypass pre-commit and commit-msg hooks."`
	Signoff       bool              `config:"commit.signoff" help:"Add Signed-off-by trailer to the commit message"`
	ModuleMessage map[string]string `name:"module-message" placeholder:"PATH=MSG" help:"Per-submodule commit message override (repeatable)"`
}

func (*commitCreateCmd) Help() string {
	name := cli.Name()
	return text.Dedent(fmt.Sprintf(`
		Staged changes are committed to the current branch.
		Branches upstack are restacked if necessary.
		Use this as a shortcut for 'git commit'
		followed by '%[1]s upstack restack'.

		An editor is opened to edit the commit message.
		Use the -m/--message or -F/--file option to specify the message
		without opening an editor.
		Git hooks are run unless the --no-verify flag is given.

		Use the -a/--all flag to stage all changes before committing.

		Use the --fixup flag to create a new commit that will be merged
		into another commit when run with 'git rebase --autosquash'.
		See also, the '%[1]s commit fixup' command, which is preferable
		when you want to apply changes to an older commit.
	`, name))
}

func (cmd *commitCreateCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	submoduleTracker SubmoduleTracker,
	submoduleApplier SubmoduleApplier,
	restackHandler RestackHandler,
) error {
	// Pre-commit submodule work runs before the parent commit so the
	// parent commit can include any updated gitlinks in a single commit.
	// Fixup mode is treated like create (we just commit in subs; the
	// recursive --fixup propagation is deferred).
	if cmd.Fixup == "" {
		currentForState, _ := wt.CurrentBranch(ctx)
		if currentForState != "" {
			if _, err := submoduleApplier.PreCommitSubmodules(ctx, currentForState, submodule.CommitModeCreate, submodule.CommitMessageSource{
				Message:       cmd.Message,
				MessageFile:   cmd.MessageFile,
				ModuleMessage: cmd.ModuleMessage,
				Signoff:       cmd.Signoff,
				NoVerify:      cmd.NoVerify,
				All:           cmd.All,
			}); err != nil {
				return fmt.Errorf("submodule pre-commit: %w", err)
			}
		}
	}

	if err := wt.Commit(ctx, git.CommitRequest{
		Message:     cmd.Message,
		MessageFile: cmd.MessageFile,
		All:         cmd.All,
		AllowEmpty:  cmd.AllowEmpty,
		Fixup:       cmd.Fixup,
		NoVerify:    cmd.NoVerify,
		Signoff:     cmd.Signoff,
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

	if err := submoduleTracker.RecordBranchState(
		ctx, currentBranch,
	); err != nil {
		log.Warn("Could not record submodule associations",
			"error", err)
	}

	return restackHandler.RestackUpstack(ctx, currentBranch, &restack.UpstackOptions{
		SkipStart: true,
	})
}

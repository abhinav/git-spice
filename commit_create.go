package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/claude"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type commitCreateCmd struct {
	All           bool   `short:"a" help:"Stage all changes before committing."`
	AllowEmpty    bool   `help:"Create a new commit even if it contains no changes."`
	ClaudeSummary bool   `help:"Generate commit message using Claude AI."`
	Fixup         string `help:"Create a fixup commit. See also 'gs commit fixup'." placeholder:"COMMIT"`
	Message       string `short:"m" placeholder:"MSG" help:"Use the given message as the commit message."`
	NoVerify      bool   `help:"Bypass pre-commit and commit-msg hooks."`
	Signoff       bool   `config:"commit.signoff" help:"Add Signed-off-by trailer to the commit message"`
}

func (*commitCreateCmd) Help() string {
	return text.Dedent(`
		Staged changes are committed to the current branch.
		Branches upstack are restacked if necessary.
		Use this as a shortcut for 'git commit'
		followed by 'gs upstack restack'.

		An editor is opened to edit the commit message.
		Use the -m/--message option to specify the message
		without opening an editor.
		Git hooks are run unless the --no-verify flag is given.

		Use the -a/--all flag to stage all changes before committing.

		Use the --fixup flag to create a new commit that will be merged
		into another commit when run with 'git rebase --autosquash'.
		See also, the 'gs commit fixup' command, which is preferable
		when you want to apply changes to an older commit.
	`)
}

func (cmd *commitCreateCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	wt *git.Worktree,
	restackHandler RestackHandler,
) error {
	// Generate commit message with Claude if requested.
	message := cmd.Message
	template := ""
	if cmd.ClaudeSummary && message != "" {
		log.Warn("--claude-summary ignored because -m/--message was provided")
	}
	if cmd.ClaudeSummary && message == "" {
		result, err := cmd.generateCommitMessage(ctx, log, view, wt)
		if err != nil {
			if errors.Is(err, errCommitCancelled) {
				return err
			}
			return fmt.Errorf("generate commit message: %w", err)
		}
		if result.Edit {
			// Use as template to open editor with pre-filled content.
			template = result.Message
		} else {
			message = result.Message
		}
	}

	if err := wt.Commit(ctx, git.CommitRequest{
		Message:    message,
		Template:   template,
		All:        cmd.All,
		AllowEmpty: cmd.AllowEmpty,
		Fixup:      cmd.Fixup,
		NoVerify:   cmd.NoVerify,
		Signoff:    cmd.Signoff,
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

// errCommitCancelled indicates the user cancelled the commit message generation.
var errCommitCancelled = errors.New("commit cancelled")

// commitMessageResult holds the result of generating a commit message.
type commitMessageResult struct {
	Message string
	Edit    bool // If true, open editor with Message as template.
}

func (cmd *commitCreateCmd) generateCommitMessage(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	wt *git.Worktree,
) (commitMessageResult, error) {
	// Get staged diff.
	diffText, err := wt.DiffStaged(ctx)
	if err != nil {
		return commitMessageResult{}, fmt.Errorf("get staged diff: %w", err)
	}

	if diffText == "" {
		return commitMessageResult{}, errors.New("no staged changes")
	}

	// Prepare diff for Claude.
	prepared, err := claude.PrepareDiff(diffText, &claude.PrepareDiffOptions{
		Log: log,
	})
	if err != nil {
		return commitMessageResult{}, err
	}

	// Build prompt and run.
	prompt := claude.BuildCommitPrompt(prepared.Config, prepared.FilteredDiff)

	fmt.Fprint(view, "Generating commit message with Claude... ")
	response, err := prepared.Client.SendPromptWithModel(
		ctx, prompt, prepared.Config.Models.Commit,
	)
	fmt.Fprintln(view, "done")
	if err != nil {
		return commitMessageResult{}, claude.RunClaudeError(err)
	}

	// Parse the response to extract subject and body.
	subject, body := claude.ParseTitleBody(response)

	// Show preview and get user choice.
	return showCommitPreview(view, subject, body)
}

// showCommitPreview shows the generated commit message and lets user accept/edit.
func showCommitPreview(
	view ui.View,
	subject, body string,
) (commitMessageResult, error) {
	// Format the commit message with proper line wrapping.
	message := claude.FormatCommitMessage(subject, body)

	// For non-interactive mode, just return the message.
	if !ui.Interactive(view) {
		return commitMessageResult{Message: message}, nil
	}

	// Show preview of the actual formatted message.
	fmt.Fprintln(view, "")
	fmt.Fprintln(view, "=== Claude suggests ===")
	fmt.Fprintln(view, message)
	fmt.Fprintln(view, "=======================")
	fmt.Fprintln(view, "")

	// Ask for confirmation.
	type choice int
	const (
		choiceAccept choice = iota
		choiceEdit
		choiceCancel
	)

	var selected choice
	field := ui.NewSelect[choice]().
		WithTitle("Action").
		WithValue(&selected).
		WithOptions(
			ui.SelectOption[choice]{Label: "Accept", Value: choiceAccept},
			ui.SelectOption[choice]{Label: "Edit in $EDITOR", Value: choiceEdit},
			ui.SelectOption[choice]{Label: "Cancel", Value: choiceCancel},
		)

	if err := ui.Run(view, field); err != nil {
		return commitMessageResult{}, err
	}

	switch selected {
	case choiceAccept:
		return commitMessageResult{Message: message}, nil
	case choiceEdit:
		return commitMessageResult{Message: message, Edit: true}, nil
	default: // choiceCancel
		return commitMessageResult{}, errCommitCancelled
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/xec"
)

type repoExclusiveCmd struct {
	Force bool `help:"Park worktrees even if they have uncommitted changes (changes are discarded)"`

	Command []string `arg:"" optional:"" passthrough:"" name:"command" help:"Command to run with the repository to itself"`
}

func (*repoExclusiveCmd) Help() string {
	return text.Dedent(`
		Runs a command with the whole repository to itself: it parks every
		worktree (see 'gs repo park'), runs the command, then restores the
		worktrees (see 'gs repo restore').

		The worktrees are always restored, even if the command fails, so
		this is the safe way to take exclusive mode for a single command.

		Separate the command from this one's flags with '--', for example:

			gs repo exclusive -- git rebase -i main
	`)
}

func (cmd *repoExclusiveCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	repo *git.Repository,
	store *state.Store,
) error {
	// Passthrough parsing keeps the '--' separator as the first token;
	// drop it so it is not treated as the command to run.
	command := cmd.Command
	if len(command) > 0 && command[0] == "--" {
		command = command[1:]
	}
	if len(command) == 0 {
		return errors.New("no command given; pass one after '--', " +
			"e.g. 'gs repo exclusive -- git status'")
	}

	if err := (&repoParkCmd{Force: cmd.Force}).Run(ctx, log, wt, repo, store); err != nil {
		return fmt.Errorf("park: %w", err)
	}

	// Run the command, then always restore, so the worktrees come back
	// even when the command fails.
	runErr := xec.Command(ctx, log, command[0], command[1:]...).
		WithDir(wt.RootDir()).
		WithStdin(os.Stdin).
		WithStdout(os.Stdout).
		WithStderr(os.Stderr).
		Run()

	if err := (&repoRestoreCmd{}).Run(ctx, log, repo, store); err != nil {
		if runErr != nil {
			return errors.Join(
				fmt.Errorf("command: %w", runErr),
				fmt.Errorf("restore: %w", err),
			)
		}
		return fmt.Errorf("restore: %w", err)
	}

	if runErr != nil {
		return fmt.Errorf("command failed: %w", runErr)
	}
	return nil
}

package gitspice

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/git-spice/internal/git"
	"go.abhg.dev/git-spice/internal/text"
)

type commitCreateCmd struct {
	All     bool   `short:"a" help:"Stage all changes before committing."`
	Message string `short:"m" help:"Use the given message as the commit message."`
}

func (*commitCreateCmd) Help() string {
	return text.Dedent(`
		Commits the staged changes to the current branch,
		restacking upstack branches if necessary.
		Use this to keep upstack branches in sync
		as you update a branch in the middle of the stack.
	`)
}

func (cmd *commitCreateCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	if err := repo.Commit(ctx, git.CommitRequest{
		Message: cmd.Message,
		All:     cmd.All,
	}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// TODO: handle not tracked
	return (&upstackRestackCmd{}).Run(ctx, log, opts)
}

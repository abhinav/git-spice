package main

import (
	"context"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/text"
)

type logShortCmd struct {
	All bool `short:"a" long:"all" help:"Show all tracked branches, not just the current stack."`
}

func (*logShortCmd) Help() string {
	return text.Dedent(`
		Provides a tree view of the branches in the current stack,
		both upstack and downstack from it.
		Use with the -a flag to show all tracked branches.
	`)
}

func (cmd *logShortCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) (err error) {
	return branchLog(ctx, &branchLogOptions{
		All:     cmd.All,
		Log:     log,
		Globals: opts,
	})
}

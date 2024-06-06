package main

import (
	"context"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/text"
)

type logShortCmd struct {
	branchLogCmd
}

func (*logShortCmd) Help() string {
	return text.Dedent(`
		Provides a tree view of the branches in the current stack,
		both upstack and downstack from it.
		By default, branches upstack and downstack from the current
		branch are shown.
		Use with the -a/--all flag to show all tracked branches.
	`)
}

func (cmd *logShortCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) (err error) {
	return cmd.run(ctx, &branchLogOptions{
		Log:     log,
		Globals: opts,
	})
}

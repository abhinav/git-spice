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
		Only branches that are upstack and downstack from the current
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

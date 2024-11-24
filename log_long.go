package main

import (
	"context"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type logLongCmd struct {
	branchLogCmd
}

func (*logLongCmd) Help() string {
	return text.Dedent(`
		Only branches that are upstack and downstack from the current
		branch are shown.
		Use with the -a/--all flag to show all tracked branches.
	`)
}

func (cmd *logLongCmd) Run(ctx context.Context, log *log.Logger, view ui.View) (err error) {
	return cmd.run(ctx, view, &branchLogOptions{
		Log:     log,
		Commits: true,
	})
}

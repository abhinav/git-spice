package main

import (
	"context"

	"go.abhg.dev/gs/internal/msggen"
	"go.abhg.dev/gs/internal/scriptrun"
)

type internalFillMessageCmd struct {
	Operation string `arg:""`
	File      string `arg:""`
}

func (cmd *internalFillMessageCmd) Run(ctx context.Context, runner *scriptrun.Runner) error {
	return msggen.RunCommitEditor(ctx, &msggen.EditRequest{
		Operation: cmd.Operation,
		File:      cmd.File,
	}, runner)
}

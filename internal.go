package main

import (
	"context"
)

type internalCmd struct {
	AutostashPop internalAutostashPop `cmd:""`
}

type internalAutostashPop struct {
	Hash string `name:"hash" arg:"" required:""`
}

func (cmd *internalAutostashPop) Run(ctx context.Context, handler AutostashHandler) error {
	return handler.RestoreAutostash(ctx, cmd.Hash)
}

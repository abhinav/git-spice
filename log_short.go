package main

import (
	"context"

	"github.com/charmbracelet/log"
)

type logShortCmd struct{}

func (cmd *logShortCmd) Run(ctx context.Context, log *log.Logger) error {
	panic("TODO")
}

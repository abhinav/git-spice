package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/peterbourgon/ff/v3/ffcli"
)

type submitCmd struct {
	Stdin  io.Writer
	Stdout io.Writer
	Stderr io.Writer

	Main *mainConfig
}

func (cmd *submitCmd) Command() *ffcli.Command {
	flag := flag.NewFlagSet("git stack submit", flag.ContinueOnError)
	flag.SetOutput(cmd.Stderr)
	cmd.Main.RegisterFlags(flag)

	return &ffcli.Command{
		Name:      "submit",
		UsageFunc: usageText(_submitUsage),
		FlagSet:   flag,
		Exec:      cmd.Exec,
	}
}

func (cmd *submitCmd) Exec(_ context.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected arguments: %s", args)
	}

	return nil
}

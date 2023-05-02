package main

import (
	"context"
	"fmt"
	"io"

	"github.com/peterbourgon/ff/v3/ffcli"
)

var _version = "dev"

type versionCmd struct {
	Stdout io.Writer
}

func (cmd *versionCmd) Command() *ffcli.Command {
	return &ffcli.Command{
		Name:      "version",
		Exec:      cmd.Exec,
		UsageFunc: usageText("USAGE\n  git stack version"),
	}
}

func (cmd *versionCmd) Exec(ctx context.Context, args []string) error {
	fmt.Fprintln(cmd.Stdout, "git-stack", _version)
	fmt.Fprintln(cmd.Stdout, "Copyright (C) 2023 Abhinav Gupta")
	fmt.Fprintln(cmd.Stdout, "  <https://github.com/abhinav/git-stack>")
	fmt.Fprintln(cmd.Stdout, "git-stack comes with ABSOLUTELY NO WARRANTY.")
	fmt.Fprintln(cmd.Stdout, "This is free software, and you are welcome to redistribute it")
	fmt.Fprintln(cmd.Stdout, "under certain conditions. See source for details.")
	return nil
}

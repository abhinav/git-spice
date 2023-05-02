package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/google/go-github/v52/github"
	"github.com/peterbourgon/ff/v3/ffcli"
	"golang.org/x/oauth2"
)

func main() {
	cmd := &mainCmd{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	os.Exit(cmd.Run(os.Args[1:]))
}

type mainConfig struct {
	Dir string
}

func (cfg *mainConfig) RegisterFlags(flag *flag.FlagSet) {
	flag.StringVar(&cfg.Dir, "C", "", "")
}

type mainCmd struct {
	Stdin  io.Writer
	Stdout io.Writer
	Stderr io.Writer

	config     mainConfig
	version    bool // -version flag is not part of mainConfig
	versionCmd *ffcli.Command
}

func (cmd *mainCmd) Run(args []string) (exitCode int) {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GITHUB_TOKEN")}, // TODO
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)
	_ = client // TODO: parse, build client, run

	cli := cmd.Command()
	if err := cli.ParseAndRun(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}

		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

func (cmd *mainCmd) Command() *ffcli.Command {
	flag := flag.NewFlagSet("git stack", flag.ContinueOnError)
	flag.SetOutput(cmd.Stderr)
	cmd.config.RegisterFlags(flag)
	flag.BoolVar(&cmd.version, "version", false, "")

	cli := &ffcli.Command{
		Name:      "git stack",
		FlagSet:   flag,
		Exec:      cmd.Exec,
		UsageFunc: usageText(_mainUsage),
	}

	cmd.versionCmd = (&versionCmd{
		Stdout: cmd.Stdout,
	}).Command()

	cli.Subcommands = []*ffcli.Command{
		(&submitCmd{
			Stdin:  cmd.Stdin,
			Stdout: cmd.Stdout,
			Stderr: cmd.Stderr,
			Main:   &cmd.config,
		}).Command(),
		cmd.versionCmd,
	}
	return cli
}

func (cmd *mainCmd) Exec(ctx context.Context, args []string) error {
	if cmd.version {
		return cmd.versionCmd.Exec(ctx, args)
	}

	return flag.ErrHelp
}

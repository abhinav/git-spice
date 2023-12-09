// git-stack is a command line tool to manage a stack of GitHub pull requests.
package main

import (
	"context"
	"io"
	"log"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/kong"
	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

func main() {
	log := log.New(io.Discard, "", 0)

	var cmd mainCmd
	ctx := kong.Parse(
		&cmd,
		kong.Writers(os.Stdout, os.Stderr),
		kong.Name("git stack"),
		kong.Description("git-stack is a command line tool to manage stacks of GitHub pull requests."),
		kong.UsageOnError(),
		kong.Bind(log),
		kong.AutoGroup(func(parent kong.Visitable, flag *kong.Flag) *kong.Group {
			if node, ok := parent.(*kong.Node); ok {
				first, sz := utf8.DecodeRuneInString(node.Name)
				if sz == 0 {
					return nil
				}

				titleName := strings.ToUpper(string(first)) + node.Name[sz:]
				return &kong.Group{
					Key:   node.Name,
					Title: titleName + " flags:",
				}
			}
			return nil
		}),
	)

	ctx.FatalIfErrorf(ctx.Run())
}

type mainCmd struct {
	// Flags with side effects
	// that are never used directly.
	Dir     kong.ChangeDirFlag `short:"C" placeholder:"DIR" help:"Change to directory before doing anything"`
	Version versionFlag        `help:"Print version information and quit"`

	Token   string      `name:"token" env:"GITHUB_TOKEN" help:"GitHub API token"`
	Verbose verboseFlag `short:"v" help:"Enable verbose output"`

	// Commands
	SubmitCmd  submitCmd  `name:"submit" cmd:"" help:"Submit a stack of pull requests"`
	VersionCmd versionCmd `name:"version" cmd:"" help:"Print version information"`
}

func (cmd *mainCmd) AfterApply() error {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: cmd.Token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// TODO: inject dependencies based on command being run.
	cmd.SubmitCmd.gh = client
	return nil
}

type verboseFlag bool

func (verboseFlag) BeforeApply(app *kong.Kong, log *log.Logger) error {
	log.SetOutput(app.Stderr)
	return nil
}

package main

import (
	"log"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/gh"
	"golang.org/x/oauth2"
)

type globalOptions struct {
	Token string `name:"token" env:"GITHUB_TOKEN" help:"GitHub API token; defaults to $GITHUB_TOKEN"`
}

type rootCmd struct {
	globalOptions

	// Flags with side effects that are never used directly.
	Verbose bool `short:"v" help:"Enable verbose output"`

	Repo repoCmd `cmd:"" aliases:"r" group:"Repository"`

	Branch branchCmd `cmd:"" aliases:"b" group:"Branch"`

	Version    versionFlag `help:"Print version information and quit"`
	VersionCmd versionCmd  `cmd:"version" name:"version" help:"Print version information"`
}

func (cmd *rootCmd) AfterApply(kctx *kong.Context, log *log.Logger) error {
	// TODO: Debug versus info logging
	// if !cmd.Verbose {
	// 	log.SetOutput(io.Discard)
	// }

	var tokenSource oauth2.TokenSource = &gh.CLITokenSource{}
	if token := cmd.Token; token != "" {
		tokenSource = oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
	}

	kctx.BindTo(tokenSource, (*oauth2.TokenSource)(nil))
	return nil
}

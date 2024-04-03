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

	// Flags with side effects whose values are never accesssed directly.
	Verbose bool               `short:"v" help:"Enable verbose output"`
	Dir     kong.ChangeDirFlag `short:"C" placeholder:"DIR" help:"Change to DIR before doing anything"`
	Version versionFlag        `help:"Print version information and quit"`

	Repo struct {
		Init repoInitCmd `cmd:"" aliases:"i" help:"Initialize a repository for stacking"`
	} `cmd:"" aliases:"r" group:"Repository"`

	Branch struct {
		Track   branchTrackCmd   `cmd:"" aliases:"tr" help:"Begin tracking a branch with gs"`
		Untrack branchUntrackCmd `cmd:"" aliases:"utr" help:"Stop tracking a branch with gs"`

		// Creation and destruction
		Create branchCreateCmd `cmd:"" aliases:"c" help:"Create a new branch"`
		Delete branchDeleteCmd `cmd:"" aliases:"de" help:"Delete the current branch"`

		// Mutation
		Edit   branchEditCmd   `cmd:"" aliases:"e" help:"Edit the current branch"`
		Rename branchRenameCmd `cmd:"" aliases:"r" help:"Rename the current branch"`
	} `cmd:"" aliases:"b" group:"Branch"`

	Commit struct {
		Create commitCreateCmd `cmd:"" aliases:"c" help:"Create a new commit"`
		Amend  commitAmendCmd  `cmd:"" aliases:"a" help:"Amend the current commit"`
	} `cmd:"" aliases:"c" group:"Commit"`

	Up       branchUpCmd       `cmd:"" group:"Movement" help:"Move up the stack"`
	Down     branchDownCmd     `cmd:"" group:"Movement" help:"Move down the stack"`
	Top      branchTopCmd      `cmd:"" group:"Movement" help:"Move to the top of the stack"`
	Bottom   branchBottomCmd   `cmd:"" group:"Movement" help:"Move to the bottom of the stack"`
	Checkout branchCheckoutCmd `cmd:"" aliases:"co" group:"Movement" help:"Checkout a specific pull request"`
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

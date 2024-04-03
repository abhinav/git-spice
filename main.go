// gs is a command line tool to manage a stack of GitHub pull requests.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"go.abhg.dev/gs/internal/gh"
	"golang.org/x/oauth2"
)

var _version = "dev"

func main() {
	log := zerolog.New(&zerolog.ConsoleWriter{
		Out:          os.Stderr,
		NoColor:      !isatty.IsTerminal(os.Stderr.Fd()),
		PartsExclude: []string{"time"},
	}).Level(zerolog.InfoLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt)
	go func() {
		select {
		case <-sigc:
			log.Info().Msg("Cleaning up. Press Ctrl-C again to exit immediately.")
			cancel()
		case <-ctx.Done():
		}
	}()

	var cmd mainCmd
	parser, err := kong.New(&cmd,
		kong.Name("gs"),
		kong.Description("gs is a command line tool to manage stacks of GitHub pull requests."),
		kong.Bind(&log, &cmd.globalOptions),
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.UsageOnError(),
	)
	if err != nil {
		panic(err)
	}

	// For each leaf subcommand, define a combined shorthand alias.
	// For example, if the command was "branch (b) create (c)",
	// the shorthand would be "bc".
	// For commands with multiple aliases, only the first is used.
	shorthands := make(map[string]*kong.Node)
	for _, n := range parser.Model.Leaves(false) {
		if n.Type != kong.CommandNode || len(n.Aliases) < 1 {
			continue
		}

		var fragments []string
		for c := n; c != nil && c.Type == kong.CommandNode; c = c.Parent {
			if len(c.Aliases) < 1 {
				panic(fmt.Sprintf("expected an alias for %q (%v)", c.Name, c.Path()))
			}
			fragments = append(fragments, c.Aliases[0])
			// TODO: handle parent without an alias
		}
		if len(fragments) < 2 {
			// If the command is already a single word, don't add an alias.
			continue
		}

		slices.Reverse(fragments)
		shorthand := strings.Join(fragments, "")

		if other, ok := shorthands[shorthand]; ok {
			panic(fmt.Sprintf("shorthand %q for %v is already in use by %v", shorthand, n.Path(), other.Path()))
		}
		shorthands[shorthand] = n

		// TODO: new node that calls the original node
		parser.Model.Children = append(parser.Model.Children, &kong.Node{
			Type:        kong.CommandNode,
			Parent:      parser.Model.Node,
			Name:        shorthand,
			Help:        n.Help,
			Detail:      n.Detail,
			Flags:       n.Flags,
			Positional:  n.Positional,
			Target:      n.Target,
			Tag:         n.Tag,
			Passthrough: n.Passthrough,
			Active:      n.Active,
			Hidden:      true,
		})
	}

	kctx, err := parser.Parse(os.Args[1:])
	parser.FatalIfErrorf(err)

	kctx.FatalIfErrorf(kctx.Run())
}

type globalOptions struct {
	Token string `name:"token" env:"GITHUB_TOKEN" help:"GitHub API token; defaults to $GITHUB_TOKEN"`
}

type mainCmd struct {
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

	Up       upCmd       `cmd:"" group:"Movement" help:"Move up the stack"`
	Down     downCmd     `cmd:"" group:"Movement" help:"Move down the stack"`
	Top      topCmd      `cmd:"" group:"Movement" help:"Move to the top of the stack"`
	Bottom   bottomCmd   `cmd:"" group:"Movement" help:"Move to the bottom of the stack"`
	Checkout checkoutCmd `cmd:"" aliases:"co" group:"Movement" help:"Checkout a specific pull request"`
}

func (cmd *mainCmd) AfterApply(kctx *kong.Context, logger *zerolog.Logger) error {
	if cmd.Verbose {
		*logger = logger.Level(zerolog.DebugLevel)
	}

	var tokenSource oauth2.TokenSource = &gh.CLITokenSource{}
	if token := cmd.Token; token != "" {
		tokenSource = oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
	}

	kctx.BindTo(tokenSource, (*oauth2.TokenSource)(nil))
	return nil
}

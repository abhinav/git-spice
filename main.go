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

	var cmd rootCmd
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

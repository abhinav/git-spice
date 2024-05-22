package gitspice

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
)

// dumpMarkdownCmd is a hidden flag that makes the command
// dupm a Markdown reference to stdout and exit.
type dumpMarkdownCmd struct {
	Out string `help:"Output file" type:"path"`

	w io.Writer
}

func (cmd *dumpMarkdownCmd) Run(app *kong.Kong) (err error) {
	var w io.Writer = os.Stdout
	if cmd.Out != "" && cmd.Out != "-" {
		f, err := os.Create(cmd.Out)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer func() {
			err = errors.Join(err, f.Close())
		}()
		w = f
	}

	buf := bufio.NewWriter(w)
	defer func() {
		err = errors.Join(err, buf.Flush())
	}()

	cmd.w = buf
	cmd.dump(app.Model)
	return nil
}

func (cmd *dumpMarkdownCmd) println(args ...interface{}) {
	fmt.Fprintln(cmd.w, args...)
}

func (cmd *dumpMarkdownCmd) print(args ...interface{}) {
	fmt.Fprint(cmd.w, args...)
}

func (cmd *dumpMarkdownCmd) printf(format string, args ...interface{}) {
	fmt.Fprintf(cmd.w, format, args...)
}

func (cmd dumpMarkdownCmd) dump(app *kong.Application) {
	cmd.header(1, app.Name+" command reference")

	cmd.println("```")
	cmd.println("gs" + app.Summary())
	cmd.println("```")
	cmd.println()

	if app.Help != "" {
		cmd.println(app.Help)
		cmd.println()
	}

	if app.Detail != "" {
		cmd.println(app.Detail)
		cmd.println()
	}

	cmd.print("**Global flags**\n\n")
	for _, flag := range app.Flags {
		cmd.dumpFlag(flag)
	}
	cmd.println()

	// TODO: command groups
	for _, subcmd := range app.Leaves(true) {
		cmd.dumpCommand(subcmd, 2)
	}
}

func (cmd dumpMarkdownCmd) dumpCommand(node *kong.Node, level int) {
	if node.Hidden {
		return
	}

	var parts []string
	for n := node; n != nil && n.Type == kong.CommandNode; n = n.Parent {
		parts = append(parts, n.Name)
	}
	parts = append(parts, "gs")
	slices.Reverse(parts)

	cmd.header(level, strings.Join(parts, " "))
	cmd.println("```")
	cmd.println("gs " + node.Summary())
	cmd.println("```")
	cmd.println()

	if node.Help != "" {
		cmd.println(node.Help)
		cmd.println()
	}

	if node.Detail != "" {
		cmd.println(node.Detail)
		cmd.println()
	}

	if len(node.Positional) > 0 {
		cmd.print("**Arguments**\n\n")
		for _, arg := range node.Positional {
			cmd.dumpArg(arg)
		}
		cmd.println()
	}
	if len(node.Flags) > 0 {
		// TODO: flag groups
		cmd.print("**Flags**\n\n")
		for _, flag := range node.Flags {
			cmd.dumpFlag(flag)
		}
		cmd.println()
	}

	for _, child := range node.Children {
		cmd.dumpCommand(child, level+1)
	}
}

func (cmd dumpMarkdownCmd) dumpArg(arg *kong.Positional) {
	cmd.printf("* `%s`: %s\n", arg.Name, arg.Help)
}

func (cmd dumpMarkdownCmd) dumpFlag(flag *kong.Flag) {
	if flag.Hidden {
		return
	}

	name := flag.Name

	cmd.print("* ")

	// short flag
	if flag.Short != 0 {
		cmd.printf("`-%c`, ", flag.Short)
	}

	// long flag
	cmd.print("`--")
	if flag.IsBool() && flag.Tag.Negatable {
		cmd.print("[no-]")
	}
	cmd.print(name)
	// =value
	if !flag.IsBool() && !flag.IsCounter() {
		cmd.printf("=%s", flag.FormatPlaceHolder())
	}

	cmd.printf("`: %s\n", flag.Help)
}

func (cmd dumpMarkdownCmd) header(level int, text string) {
	cmd.printf("%s %s\n\n", strings.Repeat("#", level), text)
}

//go:build dumpmd

package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/cli/shorthand"
)

// dumpMarkdownCmd is a hidden commnad that dumps
// a Markdown reference to stdout and exit.
type dumpMarkdownCmd struct {
	Ref        string `name:"ref" help:"Output file for command reference."`
	Shorthands string `name:"shorthands" help:"Output file for shorthands table."`
}

func (cmd *dumpMarkdownCmd) Run(app *kong.Kong, shorts *shorthand.BuiltinSource) (err error) {
	ref, err := os.Create(cmd.Ref)
	if err != nil {
		return err
	}
	defer func() { _ = ref.Close() }()

	d := cliDumper{w: ref}
	d.dump(app.Model)

	if cmd.Shorthands != "" {
		f, err := os.Create(cmd.Shorthands)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		dumpShorthands(f, shorts)
	}
	return nil
}

func dumpShorthands(w io.Writer, shorts *shorthand.BuiltinSource) {
	keys := slices.Sorted(shorts.Keys())

	var t table
	t.appendHeaders("Shorthand", "Long form")
	for _, key := range keys {
		cmd := cmdFullName(shorts.Node(key))
		link := fmt.Sprintf("[%v](/cli/reference.md#%v)", cmd, strings.ReplaceAll(cmd, " ", "-"))
		t.addRow("gs "+key, link)
	}
	t.dump(w)
}

type table struct {
	headers []string
	rows    [][]string

	headerColumn bool
}

func (t *table) appendHeaders(headers ...string) {
	t.headers = append(t.headers, headers...)
}

func (t *table) addRow(row ...string) {
	t.rows = append(t.rows, row)
}

func (t *table) dump(w io.Writer) {
	fmt.Fprint(w, "|")
	for _, h := range t.headers {
		fmt.Fprintf(w, " **%s** |", h)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "|", strings.Repeat(" --- |", len(t.headers)))
	for _, row := range t.rows {
		if t.headerColumn {
			row[0] = "**" + row[0] + "**"
		}
		fmt.Fprintln(w, "|", strings.Join(row, " | "), "|")
	}
}

type cliDumper struct {
	w io.Writer
}

func (cmd *cliDumper) dump(app *kong.Application) {
	// H1 is filled by the Markdown file that includes the result.

	var groupKeys, groupTitles []string
	cmdByGroup := make(map[string][]*kong.Node)
	for _, subcmd := range app.Leaves(true) {
		var key, title string
		if grp := subcmd.ClosestGroup(); grp != nil {
			key = grp.Key
			title = grp.Title
		}

		if _, ok := cmdByGroup[key]; !ok {
			groupKeys = append(groupKeys, key)
			groupTitles = append(groupTitles, title)
		}

		cmdByGroup[key] = append(cmdByGroup[key], subcmd)
	}

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

	cmd.dumpConfigFooter(app.Node)

	for i, key := range groupKeys {
		lvl := 2
		title := groupTitles[i]
		if title != "" {
			cmd.header(lvl, title)
			lvl++
		}

		for _, subcmd := range cmdByGroup[key] {
			cmd.dumpCommand(subcmd, lvl)
		}
	}
}

func (cmd cliDumper) dumpCommand(node *kong.Node, level int) {
	if node.Hidden {
		return
	}

	cmd.header(level, cmdFullName(node))
	cmd.println("```")
	cmd.println("gs " + node.Summary())
	cmd.println("```")
	cmd.println()

	if version := node.Tag.Get("released"); version != "" {
		icon := ":material-tag:"
		text := version
		href := fmt.Sprintf("/changelog.md#%s", version)
		if version == "unreleased" {
			icon = ":material-tag-hidden:"
			text = "Unreleased"
			href = ""
		}

		cmd.printf(`<span class="mdx-badge">`)
		cmd.printf(`<span class="mdx-badge__icon">`)
		cmd.printf(`%s{ title="Released in version" }`, icon)
		cmd.printf(`</span>`)
		cmd.printf(`<span class="mdx-badge__text">`)
		if href != "" {
			cmd.printf("[%s](%s)", text, href)
		} else {
			cmd.printf("%s", text)
		}
		cmd.printf(`</span>`)
		cmd.printf("\n\n")
	}

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

	cmd.dumpConfigFooter(node)

	for _, child := range node.Children {
		cmd.dumpCommand(child, level+1)
	}
}

func (cmd cliDumper) dumpConfigFooter(node *kong.Node) {
	var configKeys []string
	for _, flag := range node.Flags {
		key := flag.Tag.Get("config")
		if key == "" {
			continue
		}
		configKeys = append(configKeys, key)
	}

	if len(configKeys) == 0 {
		return
	}

	cmd.print("**Configuration**:")
	defer cmd.printf("\n\n")

	sort.Strings(configKeys)
	for i, key := range configKeys {
		key := "spice." + key
		id := strings.ToLower(strings.ReplaceAll(key, ".", ""))
		if i > 0 {
			cmd.print(",")
		}
		cmd.printf(" [%v](/cli/config.md#%s)", key, id)
	}
}

func (cmd cliDumper) dumpArg(arg *kong.Positional) {
	cmd.printf("* `%s`: %s\n", arg.Name, arg.Help)
}

func (cmd cliDumper) dumpFlag(flag *kong.Flag) {
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
	if flag.IsBool() && flag.Tag.Negatable != "" {
		// Value is "_" for "--no-<flag>",
		// and anything else for "--<neg>".
		if flag.Tag.Negatable == "_" {
			cmd.print("[no-]")
		} else {
			// Don't need this yet, so don't bother.
			panic("not yet implemented")
		}
	}
	cmd.print(name)
	// =value
	if !flag.IsBool() && !flag.IsCounter() {
		cmd.printf("=%s", flag.FormatPlaceHolder())
	}
	cmd.printf("`")

	// If the flag can be set with an environment variable,
	// mention that here.
	if env := flag.Tag.Get("env"); env != "" {
		cmd.printf(", `$%s`", env)
	}

	// If the flag can also be set by configuration,
	// add a link to /cli/config.md#<key>.
	// This will ensure all configuration keys are documented.
	if key := flag.Tag.Get("config"); key != "" {
		key = "spice." + key
		anchor := strings.ToLower(strings.ReplaceAll(key, ".", ""))
		cmd.printf(" ([:material-wrench:{ .middle title=%q }](/cli/config.md#%s))", key, anchor)
	}

	cmd.printf(": %s\n", flag.Help)
}

func (cmd cliDumper) header(level int, text string) {
	cmd.printf("%s %s\n\n", strings.Repeat("#", level), text)
}

func (cmd cliDumper) println(args ...interface{}) {
	fmt.Fprintln(cmd.w, args...)
}

func (cmd cliDumper) print(args ...interface{}) {
	fmt.Fprint(cmd.w, args...)
}

func (cmd cliDumper) printf(format string, args ...interface{}) {
	fmt.Fprintf(cmd.w, format, args...)
}

func cmdFullName(node *kong.Node) string {
	var parts []string
	for n := node; n != nil && n.Type == kong.CommandNode; n = n.Parent {
		parts = append(parts, n.Name)
	}
	parts = append(parts, "gs")
	slices.Reverse(parts)
	return strings.Join(parts, " ")
}

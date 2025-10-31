package main

import (
	"fmt"
	"go/doc/comment"
	"io"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
)

const (
	// _helpIndent specifies the base indentation level in spaces
	// for help text content.
	// This value is used when formatting wrapped text in the right column
	// of two-column layouts (flags, arguments, commands).
	//
	// Example with _helpIndent = 2:
	//   --flag=VALUE    This is help text that wraps
	//                   to multiple lines with proper
	//                   indentation applied.
	_helpIndent = 2

	// _helpColumnPadding specifies the number of spaces between
	// the left and right columns in two-column layouts.
	// This creates visual separation between column content.
	//
	// Example with _helpColumnPadding = 4:
	//   --flag=VALUE    Help text here
	//   ^^^^^ left      ^^^^ right column
	//         column    (4 spaces separate them)
	_helpColumnPadding = 4

	// _maxLeftColumnRatio defines the maximum width of the left column
	// as a ratio of terminal width (37.5% = 0.375).
	// This prevents the left column from consuming too much horizontal space
	// in wide terminals, ensuring the right column has adequate room
	// for help text.
	//
	// For an 80-character terminal: max left width = 80 * 0.375 = 30 chars
	// For a 120-character terminal: max left width = 120 * 0.375 = 45 chars
	_maxLeftColumnRatio = 0.375

	// _minLeftColumnWidth specifies the minimum width in characters
	// for the left column in two-column layouts.
	// This ensures readability even in narrow terminals
	// by guaranteeing a baseline width for flags, commands, and arguments.
	//
	// When the left column content is shorter than this minimum,
	// the column is still sized to this width for consistent alignment.
	_minLeftColumnWidth = 30
)

// helpPrinter is a custom help printer for git-spice.
// It acts similarly to Kong's default help output to show configuration options.
func helpPrinter(_ kong.HelpOptions, ctx *kong.Context) error {
	// The default help flag text has a period at the end,
	// which doesn't match the rest of our help text.
	// Remove the period and place it in the same group
	// as the other global flags.
	if help := ctx.Model.HelpFlag; help != nil {
		help.Help = "Show help for the command"
		help.Group = &kong.Group{
			Key:   "globals",
			Title: "Global Flags:",
		}
	}

	w := newHelpWriter(ctx.Stdout)
	selected := ctx.Selected()
	if selected == nil {
		w.printApp(ctx.Model)
	} else {
		w.printCommand(ctx.Model, selected)
	}

	// For the help of the top-level command,
	// print a note about shorthand aliases.
	if len(ctx.Command()) == 0 {
		w.Print("")
		w.Print("Aliases can be combined to form shorthands for commands. For example:")
		w.Printf("  %s bc => %s branch create", ctx.Model.Name, ctx.Model.Name)
		w.Printf("  %s cc => %s commit create", ctx.Model.Name, ctx.Model.Name)
	}

	return nil
}

// helpWriter builds formatted help output with support for indentation
// and word wrapping.
type helpWriter struct {
	// indent is the current indentation prefix for all printed lines.
	indent string

	// width is the maximum line width for word wrapping.
	width int

	// out is the writer where output is written.
	out io.Writer
}

func newHelpWriter(out io.Writer) *helpWriter {
	wrapWidth := guessWidth(out)
	return &helpWriter{
		width: wrapWidth,
		out:   out,
	}
}

// TODO: io.Writer implementation that handles indentation?

// Printf renders formatted text with the current indentation.
// Resulting text should not have newlines.
func (w *helpWriter) Printf(format string, args ...any) {
	w.Print(fmt.Sprintf(format, args...))
}

// Print writes a single line of text with the current indentation.
// text should not have newlines.
func (w *helpWriter) Print(text string) {
	line := strings.TrimRight(w.indent+text, " ")
	// Ignore write errors - help output is best-effort
	_, _ = io.WriteString(w.out, line+"\n")
}

// Indent returns a new helpWriter with increased indentation.
func (w *helpWriter) Indent() *helpWriter {
	return &helpWriter{
		indent: w.indent + "  ",
		out:    w.out,
		width:  w.width - 2,
	}
}

// Wrap word-wraps the given text to fit within the current width
// and prints each line with the current indentation.
//
// Uses GoDoc formatting to perform wrapping
// that respects paragraph breaks and indentation in the input text.
func (w *helpWriter) Wrap(text string) {
	formatted := strings.TrimSpace(formatHelpText(text, "    ", w.width))
	for line := range strings.SplitSeq(formatted, "\n") {
		w.Print(line)
	}
}

func (w *helpWriter) printApp(app *kong.Application) {
	w.Printf("Usage: %s%s", app.Name, app.Summary())

	w.printNodeDetail(app.Node, true)
	cmds := app.Leaves(true)
	if len(cmds) > 0 && app.HelpFlag != nil {
		w.Print("")
		w.Printf(`Run "%s <command> --help" for more information on a command.`, app.Name)
	}
}

func (w *helpWriter) printCommand(app *kong.Application, cmd *kong.Node) {
	w.Printf("Usage: %s %s", app.Name, cmd.Summary())
	w.printNodeDetail(cmd, true)
}

func (w *helpWriter) printNodeDetail(node *kong.Node, hide bool) {
	if node.Help != "" {
		w.Print("")
		w.Wrap(node.Help)
	}
	if node.Detail != "" {
		w.Print("")
		w.Wrap(node.Detail)
	}
	if len(node.Positional) > 0 {
		w.Print("")
		w.Print("Arguments:")
		w.Indent().writePositionals(node.Positional)
	}

	w.printFlags(node, hide)

	cmds := node.Leaves(hide)
	if len(cmds) > 0 {
		for _, group := range collectCommandGroups(cmds) {
			w.Print("")
			if group.Metadata.Title != "" {
				w.Wrap(group.Metadata.Title)
			}
			if group.Metadata.Description != "" {
				w.Indent().Wrap(group.Metadata.Description)
				w.Print("")
			}

			w.Indent().writeCompactCommandList(group.Commands)
		}
	}

	// Print config-only options (hidden:"" but with config tags).
	w.writeConfigOnlyOptions(node)
}

// printFlags prints all flags for the given node.
func (w *helpWriter) printFlags(node *kong.Node, hide bool) {
	flags := node.AllFlags(hide)
	if len(flags) == 0 {
		return
	}

	for _, group := range collectFlagGroups(flags) {
		w.Print("")
		if group.Metadata.Title != "" {
			w.Wrap(group.Metadata.Title)
		}
		if group.Metadata.Description != "" {
			w.Indent().Wrap(group.Metadata.Description)
			w.Print("")
		}
		w.Indent().writeFlags(group.Flags)
	}
}

func (w *helpWriter) writeCompactCommandList(cmds []*kong.Node) {
	var rows [][2]string
	for _, cmd := range cmds {
		if cmd.Hidden {
			continue
		}
		rows = append(rows, [2]string{cmd.Path(), cmd.Help})
	}
	w.writeTwoColumns(rows)
}

func (w *helpWriter) writePositionals(args []*kong.Positional) {
	var rows [][2]string
	for _, arg := range args {
		rows = append(rows, [2]string{arg.Summary(), formatValueHelp(arg)})
	}
	w.writeTwoColumns(rows)
}

func (w *helpWriter) writeFlags(groups [][]*kong.Flag) {
	var rows [][2]string
	var haveShort bool
	for _, group := range groups {
		for _, flag := range group {
			if flag.Short != 0 {
				haveShort = true
				break
			}
		}
	}
	for i, group := range groups {
		if i > 0 {
			rows = append(rows, [2]string{"", ""})
		}
		for _, flag := range group {
			if flag.Hidden {
				continue
			}

			name, help := formatFlag(haveShort, flag)
			rows = append(rows, [2]string{name, help})
		}
	}
	w.writeTwoColumns(rows)
}

func (w *helpWriter) writeConfigOnlyOptions(node *kong.Node) {
	// Collect hidden flags that have config tags
	var configFlags []*kong.Flag
	for _, flag := range node.Flags {
		if !flag.Hidden {
			continue
		}
		key := flag.Tag.Get("config")
		if key == "" || key[0] == '@' {
			// Skip flags without config tags
			// or git config references (starting with '@').
			continue
		}
		configFlags = append(configFlags, flag)
	}

	if len(configFlags) == 0 {
		return
	}

	// Sort config flags by their config key for consistent output
	slices.SortFunc(configFlags, func(a, b *kong.Flag) int {
		return strings.Compare(a.Tag.Get("config"), b.Tag.Get("config"))
	})

	w.Print("")
	w.Wrap("Configuration (🔧):")

	var rows [][2]string
	for _, flag := range configFlags {
		name, help := formatConfig(flag)
		rows = append(rows, [2]string{name, help})
	}
	w.Indent().writeTwoColumns(rows)
}

// writeTwoColumns formats and prints rows in a two-column layout.
// The left column width is determined by the widest entry,
// capped at a maximum based on terminal width.
// Text in the right column is wrapped if it exceeds available space.
func (w *helpWriter) writeTwoColumns(rows [][2]string) {
	maxLeft := max(int(float64(w.width)*_maxLeftColumnRatio), _minLeftColumnWidth)

	// Find widest left column entry (up to maxLeft)
	leftColWidth := 0
	for _, row := range rows {
		if c := len(row[0]); c > leftColWidth && c < maxLeft {
			leftColWidth = c
		}
	}

	offsetStr := strings.Repeat(" ", leftColWidth+_helpColumnPadding)

	for _, row := range rows {
		codePrefix := strings.Repeat(" ", _helpIndent)
		rightColWidth := w.width - leftColWidth - _helpColumnPadding
		formatted := strings.TrimRight(formatHelpText(row[1], codePrefix, rightColWidth), "\n")

		lines := strings.Split(formatted, "\n")
		line := fmt.Sprintf("%-*s", leftColWidth, row[0])

		// If left column fits inline, put first line of right column on same line
		if len(row[0]) < maxLeft {
			line += fmt.Sprintf("%*s%s", _helpColumnPadding, "", lines[0])
			lines = lines[1:]
		}

		w.Print(line)
		for _, line := range lines {
			w.Printf("%s%s", offsetStr, line)
		}
	}
}

// helpFlagGroup represents a group of flags with their metadata.
type helpFlagGroup struct {
	Metadata *kong.Group
	Flags    [][]*kong.Flag
}

// collectFlagGroups organizes flags into groups based on their group key.
// Ungrouped flags are always returned first.
// Groups are returned in order of first appearance.
func collectFlagGroups(flags [][]*kong.Flag) []helpFlagGroup {
	// Track groups in order of appearance
	var groups []*kong.Group
	seenGroups := make(map[string]bool)

	// Flags grouped by their group key
	flagsByGroup := make(map[string][][]*kong.Flag)

	for _, levelFlags := range flags {
		levelFlagsByGroup := make(map[string][]*kong.Flag)

		for _, flag := range levelFlags {
			key := ""
			if flag.Group != nil {
				key = flag.Group.Key
				if !seenGroups[key] {
					groups = append(groups, flag.Group)
					seenGroups[key] = true
				}
			}

			levelFlagsByGroup[key] = append(levelFlagsByGroup[key], flag)
		}

		for key, flags := range levelFlagsByGroup {
			flagsByGroup[key] = append(flagsByGroup[key], flags)
		}
	}

	var out []helpFlagGroup
	// Ungrouped flags are always displayed first
	if ungroupedFlags, ok := flagsByGroup[""]; ok {
		out = append(out, helpFlagGroup{
			Metadata: &kong.Group{Title: "Flags:"},
			Flags:    ungroupedFlags,
		})
	}
	for _, group := range groups {
		out = append(out, helpFlagGroup{Metadata: group, Flags: flagsByGroup[group.Key]})
	}
	return out
}

// helpCommandGroup represents a group of commands with their metadata.
type helpCommandGroup struct {
	Metadata *kong.Group
	Commands []*kong.Node
}

// collectCommandGroups organizes commands into groups based on their group key.
// Ungrouped commands are always returned first.
// Groups are returned in order of first appearance.
func collectCommandGroups(nodes []*kong.Node) []helpCommandGroup {
	// Track groups in order of appearance
	var groups []*kong.Group
	seenGroups := make(map[string]struct{})

	// Nodes grouped by their group key
	nodesByGroup := make(map[string][]*kong.Node)

	for _, node := range nodes {
		var key string
		if group := node.ClosestGroup(); group != nil {
			key = group.Key
			if _, ok := seenGroups[key]; !ok {
				groups = append(groups, group)
				seenGroups[key] = struct{}{}
			}
		}
		nodesByGroup[key] = append(nodesByGroup[key], node)
	}

	var out []helpCommandGroup
	// Ungrouped nodes are always displayed first
	if ungroupedNodes, ok := nodesByGroup[""]; ok {
		out = append(out, helpCommandGroup{
			Metadata: &kong.Group{Title: "Commands:"},
			Commands: ungroupedNodes,
		})
	}
	for _, group := range groups {
		out = append(out, helpCommandGroup{Metadata: group, Commands: nodesByGroup[group.Key]})
	}
	return out
}

// formatFlag returns the formatted flag name and help text for the given flag.
//
// haveShort indicates whether any flags in the current group have short names,
// which affects alignment.
func formatFlag(haveShort bool, flag *kong.Flag) (flagName, flagHelp string) {
	var sb strings.Builder
	name := flag.Name
	isBool := flag.IsBool()
	isCounter := flag.IsCounter()

	var short string
	if flag.Short != 0 {
		short = "-" + string(flag.Short) + ", "
	} else if haveShort {
		short = "    "
	}

	if isBool && flag.Tag.Negatable == "_" {
		name = "[no-]" + name
	} else if isBool && flag.Tag.Negatable != "" {
		name += "/" + flag.Tag.Negatable
	}

	sb.WriteString(fmt.Sprintf("%s--%s", short, name))

	if !isBool && !isCounter {
		sb.WriteString("=" + flag.FormatPlaceHolder())
	}
	flagName = sb.String()

	flagHelp = formatValueHelp(flag.Value)

	// Add config annotation if this flag has a config override
	if configKey := flag.Tag.Get("config"); configKey != "" && configKey[0] != '@' {
		flagHelp = fmt.Sprintf("%s (🔧 spice.%s)", flagHelp, configKey)
	}

	return flagName, flagHelp
}

func formatConfig(flag *kong.Flag) (configName, configHelp string) {
	configName = "spice." + flag.Tag.Get("config")
	configHelp = formatValueHelp(flag.Value)
	return configName, configHelp
}

func formatValueHelp(value *kong.Value) string {
	// Skip adding environment variables if:
	// - there are no env vars to add, or
	// - the help text already contains ${env} interpolation
	if len(value.Tag.Envs) == 0 || strings.Contains(value.OrigHelp, "${env}") {
		return value.Help
	}
	suffix := "(" + formatEnvs(value.Tag.Envs) + ")"
	help := strings.TrimSuffix(value.Help, ".")
	if help == "" {
		return suffix
	}
	if help == value.Help {
		// No period was trimmed
		return help + " " + suffix
	}
	// Period was trimmed, add it back after suffix
	return help + " " + suffix + "."
}

func formatEnvs(envs []string) string {
	formatted := make([]string, len(envs))
	for i := range envs {
		formatted[i] = "$" + envs[i]
	}
	return strings.Join(formatted, ", ")
}

func guessWidth(w io.Writer) int {
	// Try to get terminal width
	type widther interface {
		Width() int
	}
	if wt, ok := w.(widther); ok {
		return wt.Width()
	}
	// Default to 80 columns
	return 80
}

func formatHelpText(text string, codePrefix string, width int) string {
	var parser comment.Parser
	doc := parser.Parse(text)
	return string((&comment.Printer{
		TextWidth:      width,
		TextCodePrefix: codePrefix,
	}).Text(doc))
}

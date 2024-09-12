// SPDX-License-Identifier: BSD-3-Clause

// Package komplete is a package for generating completions for Kong CLIs.
//
// To use it, build a Kong parser from your CLI grammar,
// and then call [Run] with it to run the completion logic.
// This will automatically determine if the CLI is being invoked
// as a completion script or as a regular command.
//
//	parser, err := kong.New(cli)
//	// ...
//	komplete.Run(parser)
//
// [Command] is provided as a convenient subcommand for generating
// completion scripts for various shells.
//
// Custom logic to predict values for flags and arguments can be provided
// through [WithPredictor]. Install a predictor with a name,
// and refer to it in your CLI grammar with the `predictor:"name"` tag.
//
//	type CLI struct {
//		Name string `help:"Name of the branch" predictor:"branches"`
//		// ...
//	}
//
//	// ...
//	komplete.Run(parser,
//		komplete.WithPredictor("branches", branchesPredictor),
//		// ...
//	)
package komplete

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/buildkite/shellwords"
	"go.abhg.dev/gs/internal/must"
)

// Implementation notes:
//
// - This package is inspired by https://github.com/WillAbides/kongplete,
//   but it is a from-scratch implementation that more-or-less reimplements
//   Kong's CLI parsing logic.
// - It previously made use of github.com/posener/complete,
//   but that required use of the now-deprecated v1 version of the package,
//   as v2 did not provide sufficient control over the completion logic.
//   So we interface with Bash directly instead.

// Command is the command to run to generate the completion script.
// It is intended to be used as a subcommand of the main CLI.
type Command struct {
	Shell string `enum:"bash,zsh,fish," arg:"" default:"" optional:"" help:"Shell to generate completions for."`
}

// Run runs the completion script generator.
// It will print the completion script to stdout and exit.
func (cmd *Command) Run(kctx *kong.Context) (err error) {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	out := bufio.NewWriter(kctx.Stdout)
	defer func() {
		err = errors.Join(err, out.Flush())
	}()

	// Guess based on basename $SHELL, or $FISH_VERSION.
	if cmd.Shell == "" {
		switch filepath.Base(os.Getenv("SHELL")) {
		case "bash":
			cmd.Shell = "bash"
		case "zsh":
			cmd.Shell = "zsh"
		case "fish":
			// Fish doesn't actually set $SHELL.
			// We have this here for completeness.
			cmd.Shell = "fish"
		}

		if cmd.Shell == "" && os.Getenv("FISH_VERSION") != "" {
			cmd.Shell = "fish"
		}

		if cmd.Shell == "" {
			return errors.New("could not guess shell, please provide one")
		}
	}

	name := kctx.Model.Name
	switch cmd.Shell {
	case "zsh":
		fmt.Fprintln(out, `autoload -U +X bashcompinit && bashcompinit`)
		fallthrough // bash and zsh use the same logic
	case "bash":
		// complete is a bash built-in that arranges for exe
		// to be called to request completions.
		//
		// exe will be called with some environment variables set.
		// We care about:
		//
		//   - COMP_LINE: the current command line
		//   - COMP_POINT: the cursor position
		//
		// The program must generate completions to stdout,
		// one per line.
		//
		// Ref:
		// https://www.gnu.org/software/bash/manual/html_node/Programmable-Completion-Builtins.html#index-complete
		// https://www.gnu.org/software/bash/manual/html_node/Bash-Variables.html#index-COMP_005fLINE
		fmt.Fprintf(out, "complete -C %s %s\n", exe, name)

	case "fish":
		fmt.Fprintf(out, "function __complete_%s\n", name)
		fmt.Fprintln(out, "    set -lx COMP_LINE (commandline -cp)")
		fmt.Fprintln(out, "    test -z (commandline -ct)")
		fmt.Fprintln(out, `    and set COMP_LINE "$COMP_LINE "`)
		fmt.Fprintf(out, "    %s\n", exe)
		fmt.Fprintln(out, "end")
		fmt.Fprintf(out, `complete -f -c %s -a "(__complete_%s)"`+"\n", name, name)

	default:
		return fmt.Errorf("unsupported shell: %s", cmd.Shell)
	}

	return nil
}

// Args holds the command line completion arguments.
type Args struct {
	// Completed is a list of arguments that have already been completed,
	// preceding the argument currently being typed.
	Completed []string

	// Last is the typed portion of the current argument.
	// Predictions will typically be matched against this.
	Last string
}

func newArgs(line string, point int) Args {
	// If the cursor is in the middle of the text,
	// drop everything up to the cursor and the word under the cursor.
	var last string
	if point < len(line) {
		line = line[:point]
	}

	if idx := strings.LastIndexByte(line, ' '); idx != -1 {
		last = line[idx+1:]
		line = line[:idx]
	} else {
		last = line
	}

	args, err := shellwords.SplitPosix(line)
	if err != nil {
		// If we can't parse the line, it's probably because
		// we're in the middle of a quoted string or similar.
		// Use basic splitting instead.
		args = strings.Fields(line)
	}
	args = args[1:] // command name

	return Args{
		Completed: args,
		Last:      last,
	}
}

func (as Args) String() string {
	return fmt.Sprintf("{completed: %q, last: %q}", as.Completed, as.Last)
}

// Predictor predicts completions for a given set of arguments.
type Predictor interface {
	Predict(Args) []string
}

type predictOr []Predictor

func (p predictOr) Predict(cargs Args) (predictions []string) {
	for _, predictor := range p {
		predictions = append(predictions, predictor.Predict(cargs)...)
	}
	return predictions
}

// PredictFunc is a function that predicts completions for a set of arguments.
type PredictFunc func(Args) []string

// Predict calls the function to predict completions.
func (f PredictFunc) Predict(cargs Args) []string {
	return f(cargs)
}

// TODO: without a global
var logf = func(string, ...any) {}

func init() {
	if os.Getenv("COMP_DEBUG") != "" {
		logf = log.New(os.Stderr, "komplete: ", 0).Printf
	}
}

// Run runs the CLI argument completer if the user has requested completions.
// Otherwise, this is a no-op.
func Run(parser *kong.Kong, opts ...Option) {
	compLine := os.Getenv("COMP_LINE")
	compPointStr := os.Getenv("COMP_POINT")
	if compLine == "" {
		return
	}
	logf("COMP_LINE: %q, COMP_POINT: %q", compLine, compPointStr)

	compPoint, err := strconv.Atoi(compPointStr)
	if err != nil {
		logf("invalid COMP_POINT (%q): %v", compPointStr, err)
		compPoint = len(compLine)
	}
	args := newArgs(compLine, compPoint)
	logf("completion arguments: %v", args)

	options := options{
		named: make(map[string]Predictor),
	}
	for _, opt := range opts {
		opt(&options)
	}

	predictions := newKongPredictor(parser.Model, options).Predict(args)
	stdout := parser.Stdout
	for _, p := range predictions {
		_, _ = fmt.Fprintln(stdout, p)
	}

	parser.Exit(0)
}

// Option customizes completion logic.
type Option func(*options)

type options struct {
	named              map[string]Predictor
	transformCompleted func([]string) []string
}

// WithPredictor adds a named predictor to the completion logic.
//
// Flags and arguments can request a predictor for their values
// by adding a `predictor:"name"` tag to the field.
//
//	type CLI struct {
//		Name string `help:"Name of the branch" predictor:"branches"`
//		// ...
//	}
//
//	komplete.Run(parser,
//		komplete.WithPredictor("branches", branchesPredictor),
//	)
func WithPredictor(name string, predictor Predictor) Option {
	return func(opts *options) {
		opts.named[name] = predictor
	}
}

// WithTransformCompleted allows modifying the list of completed arguments,
// allowing replication of any os.Args transformations.
func WithTransformCompleted(fn func([]string) []string) Option {
	// TODO: better name for this
	return func(opts *options) {
		opts.transformCompleted = fn
	}
}

// kongPredictor is a [Predictor] that interprets flags
// using Kong's CLI behaviors.
type kongPredictor struct {
	model *kong.Application

	named              map[string]Predictor // name => predictor
	transformCompleted func([]string) []string
}

var _ Predictor = (*kongPredictor)(nil)

func newKongPredictor(model *kong.Application, opts options) *kongPredictor {
	return &kongPredictor{
		model:              model,
		named:              opts.named,
		transformCompleted: opts.transformCompleted,
	}
}

func (k *kongPredictor) Predict(cargs Args) (predictions []string) {
	completed := cargs.Completed
	if k.transformCompleted != nil {
		completed = k.transformCompleted(completed)
	}

	p := k.findPredictor(k.model.Node, kong.Scan(completed...))
	if p == nil {
		return nil
	}

	// The predictor is only based on what has been fully typed.
	// We'll want to filter those predictions on the last token.
	return (&prefixPredictor{Predictor: p}).Predict(cargs)
}

func (k *kongPredictor) findPredictor(node *kong.Node, scan *kong.Scanner) Predictor {
	// Logic based on
	// https://github.com/alecthomas/kong/blob/master/context.go#L370.

	var positional int // current position in positional arguments
	allFlags := slices.Clone(node.Flags)
	usedFlags := make(map[string]struct{})

parser:
	for !scan.Peek().IsEOL() {
		token := scan.Peek()
		switch token.Type {
		case kong.UntypedToken:
			if v, ok := token.Value.(string); ok {
				switch {
				case v == "-":
					// Bare "-" is a positional argument.
					scan.Pop()
					scan.PushTyped(token.Value, kong.PositionalArgumentToken)

				case v == "--": // end of flags
					scan.Pop()
					return nil

				case strings.HasPrefix(v, "--"): // long flag
					scan.Pop()
					v = v[2:]
					flag, value, ok := strings.Cut(v, "=")
					if ok {
						scan.PushTyped(value, kong.FlagValueToken)
					}
					scan.PushTyped(flag, kong.FlagToken)

				case strings.HasPrefix(v, "-"): // short flag
					scan.Pop()
					if tail := v[2:]; tail != "" {
						scan.PushTyped(tail, kong.ShortFlagTailToken)
					}
					scan.PushTyped(v[1:2], kong.ShortFlagToken)

				default:
					// Anything that doesn't match the rest
					// is a positional argument.
					scan.Pop()
					scan.PushTyped(token.Value, kong.PositionalArgumentToken)
				}
			} else {
				scan.Pop()
				scan.PushTyped(token.Value, kong.PositionalArgumentToken)
			}

		case kong.ShortFlagTailToken:
			scan.Pop()
			if tail := token.String()[1:]; tail != "" {
				scan.PushTyped(tail, kong.ShortFlagTailToken)
			}
			scan.PushTyped(token.String()[0:1], kong.ShortFlagToken)

		case kong.FlagToken, kong.ShortFlagToken:
			f, status := k.matchFlag(allFlags, scan, token.String())
			switch status {
			case flagExpectingValue:
				return &valuePredictor{
					value: f.Value,
					named: k.named,
				}

			case flagConsumed:
				// Used flags will not be predicted,
				// but they're allowed to be repeated.
				usedFlags[f.Name] = struct{}{}

			case flagNotMatched:
				logf("unexpected flag: %v", token)
				return nil

			default:
				must.Failf("unexpected flag status: %v", status)
			}

		case kong.FlagValueToken:
			// Flag values are consumed in matchFlag.
			logf("unexpected flag value: %v", token)
			return nil

		case kong.PositionalArgumentToken:
			if positional < len(node.Positional) {
				scan.Pop()
				positional++
				continue parser // move to next token
			}

			// We're at the end of expected positional arguments.
			// Try commands next.
			arg := token.String()
			for _, child := range node.Children {
				if child.Type != kong.CommandNode {
					continue
				}

				match := child.Name == arg
				if !match && len(child.Aliases) > 0 {
					for _, alias := range child.Aliases {
						if alias == arg {
							match = true
							break
						}
					}
				}

				if match {
					scan.Pop() // consume the command
					node = child
					allFlags = append(allFlags, child.Flags...)
					positional = 0
					continue parser
				}
			}

			// None of the command matched. Check argument nodes.
			// These are positional arguments with fixed values.
			// Just skip over them.
			for _, child := range node.Children {
				if child.Type == kong.ArgumentNode {
					arg := child.Argument
					v := reflect.New(arg.Target.Type()).Elem()
					// For reference types, make sure they're initialized.
					switch v.Kind() {
					case reflect.Ptr:
						v.Set(reflect.New(v.Type().Elem()))
					case reflect.Slice:
						v.Set(reflect.MakeSlice(v.Type(), 0, 0))
					case reflect.Map:
						v.Set(reflect.MakeMap(v.Type()))
					}
					err := child.Argument.Parse(scan, v)
					if err == nil {
						positional = 0
						node = child
						continue parser
					}

					logf("failed to parse argument: %+v", err)
				}
			}

			if !scan.Peek().IsEOL() {
				// We have extra arguments. Stop predicting.
				logf("unexpected argument: %v (%v)", token, token.Type)
				return nil
			}

		default:
			logf("unexpected token: %v (%v)", token, token.Type)
			return nil
		}
	}

	var predictors []Predictor
	if positional < len(node.Positional) {
		// If we haven't yet consumed all positional arguments of the
		// current node, we can predict the next positional argument.
		predictors = append(predictors, &valuePredictor{
			value: node.Positional[positional],
			named: k.named,
		})
	}

	if len(node.Children) > 0 {
		// If there are subcommands, predict them.
		predictors = append(predictors, &subcommandPredictor{parent: node})
	}

	// Only predict the current node's flags.
	predictors = append(predictors, &flagsPredictor{
		allFlags: allFlags,
		flags:    node.Flags,
		used:     usedFlags,
	})
	return predictOr(predictors)
}

type flagStatus int

const (
	flagConsumed       flagStatus = iota // consumed flag and value
	flagExpectingValue                   // consumed flag, expecting value
	flagNotMatched                       // flag not matched
)

// matchFlag attempts to match a flag in the current command node.
// It returns flagStatus to indicate the outcome.
// The returned flag is nil if a flag was not matched.
func (k *kongPredictor) matchFlag(flags []*kong.Flag, scan *kong.Scanner, arg string) (*kong.Flag, flagStatus) {
	// TODO: we can maybe combine the traverse and predict logic.
	for _, flag := range flags {
		matched := "--"+flag.Name == arg

		if !matched && len(flag.Aliases) > 0 {
			for _, alias := range flag.Aliases {
				if "--"+alias == arg {
					matched = true
					break
				}
			}
		}

		if !matched && flag.Short != 0 {
			matched = "-"+string(flag.Short) == arg
		}

		if negFlag := negatableFlagName(flag); !matched && negFlag != "" {
			matched = negFlag == arg
		}

		if !matched {
			continue
		}

		scan.Pop() // consume the flag

		if scan.Peek().IsEOL() && !flag.IsBool() {
			// Missing value for the flag.
			// Let the caller predict the value.
			return flag, flagExpectingValue
		}

		_ = flag.Parse(scan, flag.Target) // consume the value
		return flag, flagConsumed
	}

	return nil, flagNotMatched
}

// subcommandPredictor predicts subcommands for a node
// that's been fully resolved.
type subcommandPredictor struct {
	parent *kong.Node
}

var _ Predictor = (*subcommandPredictor)(nil)

func (p *subcommandPredictor) Predict(cargs Args) []string {
	var predictions []string
	for _, child := range p.parent.Children {
		if child.Type != kong.CommandNode || child.Hidden {
			continue
		}

		if strings.HasPrefix(child.Name, cargs.Last) {
			predictions = append(predictions, child.Name)
		}

		// If an alias has been fully typed,
		// include it in the predictions so it can be completed.
		for _, alias := range child.Aliases {
			if alias == cargs.Last {
				predictions = append(predictions, alias)
			}
		}
	}

	return predictions
}

type valuePredictor struct {
	value *kong.Value
	named map[string]Predictor
}

var _ Predictor = (*valuePredictor)(nil)

func (p *valuePredictor) Predict(cargs Args) []string {
	if name := p.value.Tag.Get("predictor"); name != "" {
		if p, ok := p.named[name]; ok {
			return p.Predict(cargs)
		}
		logf("predictor not found: %s", name)
	}

	if p.value.Enum != "" {
		return p.value.EnumSlice()
	}

	return nil
}

// flagsPredictor attempts to be a bit smart:
//
//   - If there's no input, predict only flags for the current node.
//   - If there's input, also include matching aliases, and flags from parent nodes.
//   - Don't predict flags that have already been filled.
type flagsPredictor struct {
	allFlags []*kong.Flag
	flags    []*kong.Flag
	used     map[string]struct{}
}

var _ Predictor = (*flagsPredictor)(nil)

func (p *flagsPredictor) Predict(cargs Args) (predictions []string) {
	flagPrefix, _ := strings.CutPrefix(cargs.Last, "--")

	flags := p.flags
	if flagPrefix != "" {
		flags = p.allFlags
	}

	for _, flag := range flags {
		if _, ok := p.used[flag.Name]; ok || flag.Hidden {
			continue
		}

		predictions = append(predictions, "--"+flag.Name)
		if negFlag := negatableFlagName(flag); negFlag != "" {
			predictions = append(predictions, negFlag)
		}

		// Include aliases only if the user has typed a prefix.
		// Otherwise they'll just clutter the completions.
		if flagPrefix != "" {
			for _, alias := range flag.Aliases {
				if strings.HasPrefix(alias, flagPrefix) {
					predictions = append(predictions, "--"+alias)
				}
			}
		}
	}

	return predictions
}

type prefixPredictor struct {
	Predictor
}

func (p *prefixPredictor) Predict(cargs Args) (predictions []string) {
	predictions = p.Predictor.Predict(cargs)
	newPredictions := predictions[:0]
	for _, prediction := range predictions {
		if strings.HasPrefix(prediction, cargs.Last) {
			newPredictions = append(newPredictions, prediction)
		}
	}
	return newPredictions
}

func negatableFlagName(f *kong.Flag) string {
	// Kong uses "_" as a placeholder for "--no-<flag>".
	// Any other value means the flag is "--<negation>".
	switch f.Tag.Negatable {
	case "":
		return ""
	case "_":
		return "--no-" + f.Name
	default:
		return "--" + f.Tag.Negatable
	}
}

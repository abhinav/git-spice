/*
Unlike the rest of the code in this repository,
this file is made available under the BSD 3-Clause License
so that it can be copied into other projects.
License text follows.
------------------------------------------------------------------------------
BSD 3-Clause License

Copyright (c) 2024, Abhinav Gupta (https://abhinavg.net/)

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its
   contributors may be used to endorse or promote products derived from
   this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/

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
	"os"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/posener/complete"
	"go.abhg.dev/gs/internal/must"
)

// Implementation notes:
//
// - This package is inspired by https://github.com/WillAbides/kongplete,
//   but it is a from-scratch implementation that more-or-less reimplements
//   Kong's CLI parsing logic.
// - We're not using complete/v2 because it does not expose
//   the full argument list to predictors.

// Command is the command to run to generate the completion script.
// It is intended to be used as a subcommand of the main CLI.
type Command struct {
	Shell string ` enum:"bash,zsh,fish" arg:"" required:"" help:"Shell to generate completions for."`
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

	name := kctx.Model.Name
	switch cmd.Shell {
	case "bash":
		fmt.Fprintf(out, "complete -C %s %s\n", exe, name)

	case "zsh":
		fmt.Fprintln(out, `autoload -U +X bashcompinit && bashcompinit`)
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

// Run runs the CLI argument completer if the user has requested completions.
// Otherwise, this is a no-op.
func Run(parser *kong.Kong, opts ...Option) {
	options := options{
		named: make(map[string]complete.Predictor),
	}
	for _, opt := range opts {
		opt(&options)
	}

	completer := complete.New(
		parser.Model.Name,
		complete.Command{
			Args: newKongPredictor(parser.Model, options),
		},
	)
	completer.Out = parser.Stdout
	if done := completer.Complete(); done {
		parser.Exit(0)
	}
}

// Option customizes completion logic.
type Option func(*options)

type options struct {
	named              map[string]complete.Predictor
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
func WithPredictor(name string, predictor complete.Predictor) Option {
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

// kongPredictor is a [complete.Predictor] that interprets flags
// using Kong's CLI behaviors.
//
// It is intended to entirely replace complete.Command's flag and subcommand
// behavior by being used as an Args predictor.
type kongPredictor struct {
	model *kong.Application

	named              map[string]complete.Predictor // name => predictor
	transformCompleted func([]string) []string
}

var _ complete.Predictor = (*kongPredictor)(nil)

func newKongPredictor(model *kong.Application, opts options) *kongPredictor {
	return &kongPredictor{
		model:              model,
		named:              opts.named,
		transformCompleted: opts.transformCompleted,
	}
}

func (k *kongPredictor) Predict(cargs complete.Args) (predictions []string) {
	completed := cargs.Completed
	if k.transformCompleted != nil {
		completed = k.transformCompleted(completed)
	}

	p := k.findPredictor(k.model.Node, kong.Scan(completed...))
	if p == nil {
		return nil
	}
	return p.Predict(cargs)
}

func (k *kongPredictor) findPredictor(node *kong.Node, scan *kong.Scanner) complete.Predictor {
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
				complete.Log("unexpected flag: %v", token)
				return nil

			default:
				must.Failf("unexpected flag status: %v", status)
			}

		case kong.FlagValueToken:
			// Flag values are consumed in matchFlag.
			complete.Log("unexpected flag value: %v", token)
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
					_ = child.Argument.Parse(scan, child.Target) // consume the value
				}
			}

			if !scan.Peek().IsEOL() {
				// We have extra arguments. Stop predicting.
				complete.Log("unexpected argument: %v (%v)", token, token.Type)
				return nil
			}

		default:
			complete.Log("unexpected token: %v (%v)", token, token.Type)
			return nil
		}
	}

	var predictors []complete.Predictor
	if positional < len(node.Positional) {
		// If we haven't yet consumed all positional arguments of the
		// current node, we can predict the next positional argument.
		predictors = append(predictors, &valuePredictor{
			value: node.Positional[positional],
			named: k.named,
		})
	}

	var subcommands []string
	for _, child := range node.Children {
		if child.Hidden {
			continue
		}
		if child.Type == kong.CommandNode {
			subcommands = append(subcommands, child.Name)
		}
	}
	if len(subcommands) > 0 {
		predictors = append(predictors, complete.PredictSet(subcommands...))
	}

	// Only predict the current node's flags.
	predictors = append(predictors, &flagsPredictor{
		allFlags: allFlags,
		flags:    node.Flags,
		used:     usedFlags,
	})
	return complete.PredictOr(predictors...)
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

		if !matched && flag.Tag.Negatable {
			matched = "--no-"+flag.Name == arg
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

type valuePredictor struct {
	value *kong.Value
	named map[string]complete.Predictor
}

var _ complete.Predictor = (*valuePredictor)(nil)

func (p *valuePredictor) Predict(cargs complete.Args) []string {
	if name := p.value.Tag.Get("predictor"); name != "" {
		if p, ok := p.named[name]; ok {
			return p.Predict(cargs)
		}
		complete.Log("predictor not found: %s", name)
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

var _ complete.Predictor = (*flagsPredictor)(nil)

func (p *flagsPredictor) Predict(cargs complete.Args) (predictions []string) {
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
		if flag.Tag.Negatable {
			predictions = append(predictions, "--no-"+flag.Name)
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

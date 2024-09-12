// SPDX-License-Identifier: BSD-3-Clause

package komplete

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/ioutil"
)

func TestCommandRun(t *testing.T) {
	shells := []string{"bash", "zsh", "fish"}
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			var stdout bytes.Buffer

			parser, err := kong.New(
				&struct{}{},
				kong.Name("test"),
				kong.Writers(&stdout, ioutil.TestOutputWriter(t, "")),
			)
			require.NoError(t, err)

			err = (&Command{Shell: shell}).Run(&kong.Context{Kong: parser})
			require.NoError(t, err)
			assert.NotEmpty(t, stdout.String())
		})
	}

	t.Run("unknown", func(t *testing.T) {
		var stdout bytes.Buffer

		parser, err := kong.New(
			&struct{}{},
			kong.Name("test"),
			kong.Writers(&stdout, ioutil.TestOutputWriter(t, "")),
		)
		require.NoError(t, err)

		err = (&Command{Shell: "unknown"}).Run(&kong.Context{Kong: parser})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unsupported shell")

		assert.Empty(t, stdout.String())
	})
}

func TestKongPredictor(t *testing.T) {
	// Helper to construct a complete.Args from a list of arguments.
	// The last argument is what's being typed right now.
	compLine := func(args ...string) Args {
		if len(args) == 0 {
			return Args{}
		}

		completed := args[:len(args)-1]
		last := args[len(args)-1]
		return Args{
			Completed: completed,
			Last:      last,
		}
	}

	type completeCase struct {
		give Args
		want []string
	}

	type namedPredictor func(completed []string, last string) []string

	tests := []struct {
		name string

		// Grammar of the CLI parser.
		grammar any

		// Set of named predictors available to the grammar.
		named map[string]namedPredictor

		cases []completeCase
	}{
		{
			name: "simple options",
			grammar: &struct {
				Output string `short:"o"`
			}{},
			cases: []completeCase{
				{
					want: []string{"--help", "--output"},
				},
				{
					give: compLine("--o"),
					want: []string{"--output"},
				},
				{
					give: compLine("--h"),
					want: []string{"--help"},
				},
				{
					give: compLine("-o", "file", ""),
					want: []string{"--help"},
				},
				{
					give: compLine("-o", "-", ""),
					want: []string{"--help"},
				},
				// No predictions expected for the following:
				{give: compLine("--", "")},
				{give: compLine("-o", "")},
				{give: compLine("-o", "-")},
				{give: compLine("--output", "")},
				{give: compLine("--unkn")},
				{give: compLine("--unknown", "")},
			},
		},

		{
			name: "subcommands",
			grammar: &struct {
				Commit struct {
					Message string
				} `cmd:""`
				Worktree struct {
					Add    struct{} `cmd:"add"`
					Delete struct{} `cmd:"delete"`
				} `cmd:""`

				// Global options
				Verbose bool `short:"v"`
			}{},
			cases: []completeCase{
				{
					give: compLine(""),
					want: []string{
						"commit",
						"worktree",
						"--help", "--verbose",
					},
				},
				{
					give: compLine("co"),
					want: []string{"commit"},
				},
				{
					give: compLine("worktree", ""),
					want: []string{"add", "delete"},
				},
				{
					give: compLine("worktree", "a"),
					want: []string{"add"},
				},
				{
					give: compLine("commit", ""),
					want: []string{"--message"},
				},
				{
					give: compLine("commit", "--message", ""),
				},
			},
		},

		{
			// https://github.com/alecthomas/kong/blob/d315006dcaba7a02249e1ad151962365410b601e/kong_test.go#L50
			name: "branching positional arguments",
			// user create <id> <first> <last>
			// user        <id> delete
			// user        <id> rename <to>
			grammar: &struct {
				User struct {
					Create struct {
						ID    string `arg:""`
						First string `arg:""`
						Last  string `arg:""`
					} `cmd:""`

					// Branching argument.
					ID struct {
						ID   int `arg:""`
						Flag int

						Delete struct{} `cmd:""`
						Rename struct {
							To string
						} `cmd:""`
					} `arg:""`
				} `cmd:""`
			}{},
			cases: []completeCase{
				{
					want: []string{"user", "--help"},
				},
				{
					give: compLine("user", ""),
					want: []string{"create"},
				},
				{
					give: compLine("user", "42", ""),
					want: []string{"delete", "rename", "--flag"},
				},
				{
					give: compLine("user", "42", "d"),
					want: []string{"delete"},
				},
				{
					give: compLine("user", "42", "rename", ""),
					want: []string{"--to"},
				},
			},
		},

		{
			name: "command aliases",
			grammar: &struct {
				Commit struct {
					Message string `short:"m"`
				} `cmd:"" aliases:"ci"`
			}{},
			cases: []completeCase{
				{
					give: compLine(""),
					want: []string{"commit", "--help"},
				},
				{
					give: compLine("ci"),
					want: []string{"ci"},
				},
				{
					give: compLine("ci", ""),
					want: []string{"--message"},
				},
			},
		},

		{
			name: "flag/enum",
			grammar: &struct {
				LogLevel string `enum:"debug,info,warning,error" default:"info"`
			}{},
			cases: []completeCase{
				{
					want: []string{"--help", "--log-level"},
				},
				{
					give: compLine("--l"),
					want: []string{"--log-level"},
				},
				{
					give: compLine("--log-level", ""),
					want: []string{"debug", "info", "warning", "error"},
				},
				{
					give: compLine("--log-level", "d"),
					want: []string{"debug"},
				},
			},
		},

		{
			name: "predictor",
			grammar: &struct {
				Host string `arg:"" predictor:"host"`
			}{},
			named: map[string]namedPredictor{
				"host": func([]string, string) []string {
					return []string{"localhost", "example.com"}
				},
			},
			cases: []completeCase{
				{
					give: compLine(""),
					want: []string{"localhost", "example.com", "--help"},
				},
				{
					give: compLine("l"),
					want: []string{"localhost"},
				},
			},
		},
		{
			name: "negation/default",
			grammar: &struct {
				Spicy bool `negatable:""` // --spicy, --no-spicy
			}{},
			cases: []completeCase{
				{
					want: []string{"--help", "--spicy", "--no-spicy"},
				},
				{
					give: compLine("--s"),
					want: []string{"--spicy"},
				},
				{
					give: compLine("--n"),
					want: []string{"--no-spicy"},
				},
			},
		},
		{
			name: "negation/custom",
			grammar: &struct {
				Spicy bool `negatable:"mild"` // --spicy, --mild
			}{},
			cases: []completeCase{
				{
					want: []string{"--help", "--spicy", "--mild"},
				},
				{
					give: compLine("--s"),
					want: []string{"--spicy"},
				},
				{
					give: compLine("--m"),
					want: []string{"--mild"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := kong.New(tt.grammar)
			require.NoError(t, err)

			parser.Exit = func(code int) {
				t.Fatalf("exit(%d)", code)
			}

			named := make(map[string]Predictor)
			for name, predictor := range tt.named {
				named[name] = PredictFunc(func(args Args) []string {
					return predictor(args.Completed, args.Last)
				})
			}

			pred := newKongPredictor(parser.Model, options{
				named: named,
			})
			for idx, c := range tt.cases {
				name := strings.Join(c.give.Completed, " ")
				if c.give.Last != "" {
					if name != "" {
						name += " "
					}
					name += c.give.Last
				}
				if name == "" {
					name = fmt.Sprintf("%d", idx)
				}

				t.Run(name, func(t *testing.T) {
					got := pred.Predict(c.give)
					if len(c.want) == 0 {
						assert.Empty(t, got)
					} else {
						assert.Equal(t, c.want, got)
					}
				})
			}
		})
	}
}

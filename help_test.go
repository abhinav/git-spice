package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:generate go test -run ^TestHelp$ -update

func TestHelp(t *testing.T) {
	// Build Kong parser with the same configuration as main
	var cmd mainCmd
	parser, err := kong.New(&cmd,
		kong.Name("gs"),
		kong.Description("gs (git-spice) is a command line tool for stacking Git branches."),
		kong.Help(helpPrinter),
		kong.Exit(func(int) {}), // Don't actually exit in tests
		kong.Vars{
			"defaultPrompt": "true",
		},
	)
	require.NoError(t, err)

	// Collect all commands to test
	var commands []struct {
		name string
		path []string
	}

	// Add root help
	commands = append(commands, struct {
		name string
		path []string
	}{
		name: "gs",
		path: []string{},
	})

	// Collect all leaf commands (exclude hidden)
	for _, node := range parser.Model.Leaves(true) {
		// Build the command path
		var path []string
		for n := node; n != nil && n.Type == kong.CommandNode; n = n.Parent {
			path = append([]string{n.Name}, path...)
		}
		if len(path) > 0 {
			commands = append(commands, struct {
				name string
				path []string
			}{
				name: strings.Join(path, " "),
				path: path,
			})
		}
	}

	for _, tc := range commands {
		t.Run(tc.name, func(t *testing.T) {
			// Generate help output by creating a parser with a buffer
			var helpBuf bytes.Buffer
			testParser, err := kong.New(&cmd,
				kong.Name("gs"),
				kong.Description("gs (git-spice) is a command line tool for stacking Git branches."),
				kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
				kong.Help(helpPrinter),
				kong.Exit(func(int) {}),
				kong.Writers(&helpBuf, &helpBuf),
				kong.Vars{
					"defaultPrompt": "true",
				},
			)
			require.NoError(t, err)

			_, _ = testParser.Parse(append(tc.path, "--help"))

			actual := helpBuf.String()

			// Determine golden file path
			filename := strings.ReplaceAll(tc.name, " ", "_") + ".txt"
			goldenPath := filepath.Join("testdata", "help", filename)

			if *_update {
				// Update mode: write actual output to golden file
				err := os.MkdirAll(filepath.Dir(goldenPath), 0o755)
				require.NoError(t, err)
				err = os.WriteFile(goldenPath, []byte(actual), 0o644)
				require.NoError(t, err)
				t.Logf("Updated golden file: %s", goldenPath)
			} else {
				// Test mode: compare against golden file
				expected, err := os.ReadFile(goldenPath)
				require.NoError(t, err, "failed to read golden file: %s", goldenPath)
				assert.Equal(t, string(expected), actual)
			}
		})
	}
}

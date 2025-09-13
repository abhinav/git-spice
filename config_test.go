package main

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
)

// List of Git configuration sections besides "spice."
// that we read is hard-coded in spice/config.go.
// This tests that all sections that we need are covered.
func TestGitSectionsRequested(t *testing.T) {
	app, err := kong.New(
		new(mainCmd),
		kong.Vars{"defaultPrompt": "false"},
	)
	require.NoError(t, err)

	nodes := []*kong.Node{app.Model.Node}
	sections := make(map[string]struct{})
	for len(nodes) > 0 {
		node := nodes[0]
		nodes = append(nodes[1:], node.Children...)

		for _, flag := range node.Flags {
			key := flag.Tag.Get("config")
			gitKey, ok := strings.CutPrefix(key, "@")
			if !ok {
				continue
			}

			section, _, _ := git.ConfigKey(gitKey).Split()
			sections[section] = struct{}{}
		}
	}

	want := slices.Sorted(maps.Keys(sections))
	assert.ElementsMatch(t, want, spice.GitSections)
}

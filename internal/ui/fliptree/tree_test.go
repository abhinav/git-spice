package fliptree_test

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/ui/fliptree"
	"gopkg.in/yaml.v3"
	"pgregory.net/rapid"
)

var _update = flag.Bool("update", false, "update fixtures")

func TestWrite(t *testing.T) {
	type testCase struct {
		Name   string              `yaml:"name"`
		Roots  []string            `yaml:"roots"`
		Graph  map[string][]string `yaml:"graph"`
		Values map[string]string   `yaml:"values,omitempty"`

		Want        string         `yaml:"want"`
		WantOffsets map[string]int `yaml:"wantOffsets,omitempty"`
	}

	testdata, err := os.ReadFile(filepath.Join("testdata", "write.yaml"))
	require.NoError(t, err)

	var tests []testCase
	require.NoError(t, yaml.Unmarshal(testdata, &tests))

	var updated []int
	for idx, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			g := fliptree.Graph{
				Roots: tt.Roots,
				View: func(n string) string {
					v, ok := tt.Values[n]
					if !ok {
						v = n
					}
					return v
				},
				Edges: func(n string) []string {
					return tt.Graph[n]
				},
			}

			gotOffsets := make(map[string]int)
			var sb strings.Builder
			err := fliptree.Write(&sb, g, fliptree.Options{
				Style: &fliptree.Style{
					Joint: lipgloss.NewStyle(),
				},
				Offsets: gotOffsets,
			})
			require.NoError(t, err)

			got := stripTrailingSpaces(sb.String())
			if *_update {
				tests[idx].Want = got
				tests[idx].WantOffsets = gotOffsets
				updated = append(updated, idx)
				return
			}

			assert.Equal(t, tt.Want, got)
			assert.Equal(t, tt.WantOffsets, gotOffsets)
		})
	}

	if !*_update {
		return
	}

	// If there were updates, replace parts of the YAML tree.
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal(testdata, &doc))
	require.Equal(t, yaml.DocumentNode, doc.Kind)
	require.Len(t, doc.Content, 1)
	require.Equal(t, yaml.SequenceNode, doc.Content[0].Kind)

	seq := doc.Content[0].Content
	for _, testIdx := range updated {
		testNode := seq[testIdx]
		require.Equal(t, yaml.MappingNode, testNode.Kind, "test %d", testIdx)

		// Ensure empty line between each test case.
		testNode.HeadComment = "\n"

		var newChildren []*yaml.Node
		for keyIdx := 0; keyIdx < len(testNode.Content); keyIdx += 2 {
			require.Equal(t, yaml.ScalarNode, testNode.Content[keyIdx].Kind)
			switch testNode.Content[keyIdx].Value {
			case "want", "wantOffsets":
				// skip

			default:
				newChildren = append(newChildren,
					testNode.Content[keyIdx],
					testNode.Content[keyIdx+1],
				)
			}
		}

		{
			var key, value yaml.Node
			key.SetString("want")
			value.SetString(tests[testIdx].Want)
			newChildren = append(newChildren, &key, &value)
		}

		{
			var key, value yaml.Node
			key.SetString("wantOffsets")
			require.NoError(t, value.Encode(tests[testIdx].WantOffsets))
			newChildren = append(newChildren, &key, &value)
		}

		testNode.Content = newChildren
	}

	var bs bytes.Buffer
	enc := yaml.NewEncoder(&bs)
	enc.SetIndent(2)
	require.NoError(t, enc.Encode(&doc))
	require.NoError(t, enc.Close())
	require.NoError(t, os.WriteFile(filepath.Join("testdata", "write.yaml"), bs.Bytes(), 0o644))
}

func TestWriteProperty(t *testing.T) {
	rapid.Check(t, testWriteProperty)
}

func testWriteProperty(t *rapid.T) {
	allNodes := rapid.SliceOfN(rapid.String(), 1, -1).Draw(t, "nodes")
	nodeg := rapid.SampledFrom(allNodes)

	roots := rapid.SliceOfDistinct(nodeg, rapid.ID).Draw(t, "roots")
	edges := rapid.MapOf(nodeg, rapid.SliceOfDistinct(nodeg, rapid.ID)).
		Draw(t, "edges")

	g := fliptree.Graph{
		Roots: roots,
		View:  func(n string) string { return n },
		Edges: func(n string) []string { return edges[n] },
	}

	// Must not panic or infinite loop for any input.
	_ = fliptree.Write(io.Discard, g, fliptree.Options{})
}

func stripTrailingSpaces(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

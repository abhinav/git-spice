package fliptree

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/ui"
	"gopkg.in/yaml.v3"
	"pgregory.net/rapid"
)

var _update = flag.Bool("update", false, "update fixtures")

func plainStyle() *Style[string] {
	return &Style[string]{
		Joint: ui.NewStyle(),
		NodeMarker: func(string) lipgloss.Style {
			return ui.NewStyle().SetString("□")
		},
	}
}

func TestWrite(t *testing.T) {
	type testCase struct {
		Name   string              `yaml:"name"`
		Roots  []string            `yaml:"roots"`
		Graph  map[string][]string `yaml:"graph"`
		Values map[string]string   `yaml:"values,omitempty"`

		Want string `yaml:"want"`
	}

	testdata, err := os.ReadFile(filepath.Join("testdata", "write.yaml"))
	require.NoError(t, err)

	var tests []testCase
	require.NoError(t, yaml.Unmarshal(testdata, &tests))

	var updated []int
	for idx, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			nodeSet := make(map[string]struct{})
			for n, edges := range tt.Graph {
				nodeSet[n] = struct{}{}
				for _, e := range edges {
					nodeSet[e] = struct{}{}
				}
			}

			allNodes := make([]string, 0, len(nodeSet))
			for n := range nodeSet {
				allNodes = append(allNodes, n)
			}
			sort.Strings(allNodes)

			roots := make([]int, len(tt.Roots))
			for i, r := range tt.Roots {
				var ok bool
				roots[i], ok = slices.BinarySearch(allNodes, r)
				require.True(t, ok,
					"root %q not found in graph", r)
			}

			g := Graph[string]{
				Values: allNodes,
				Roots:  roots,
				View: func(n string) string {
					v, ok := tt.Values[n]
					if !ok {
						v = n
					}
					return v
				},
				Edges: func(n string) []int {
					edgeNames := tt.Graph[n]
					edges := make([]int, len(edgeNames))
					for i, e := range edgeNames {
						var ok bool
						edges[i], ok = slices.BinarySearch(allNodes, e)
						require.True(t, ok,
							"edge %q not found in graph", e)
					}
					return edges
				},
			}

			var sb strings.Builder
			err := Write(&sb, g, Options[string]{
				Style: plainStyle(),
			})
			require.NoError(t, err)

			got := stripTrailingSpaces(sb.String())
			if *_update {
				tests[idx].Want = got
				updated = append(updated, idx)
				return
			}

			assert.Equal(t, tt.Want, got)
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
			case "want":
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
	// Printable runes that aren't one of the box drawing characters.
	runeGen := rapid.Rune().
		Filter(func(r rune) bool {
			switch {
			case r == ' ':
				return true

			case !unicode.IsPrint(r),
				boxRune(r).Valid(),
				unicode.IsSpace(r),
				r == '□':
				return false

			default:
				return true
			}
		})
	stringGen := rapid.StringOfN(runeGen, 1, -1, -1)

	allNodes := rapid.SliceOfN(stringGen, 1, -1).Draw(t, "nodes")
	nodeIdxGen := rapid.IntRange(0, len(allNodes)-1)
	nodeValueGen := rapid.SampledFrom(allNodes)

	roots := rapid.SliceOfDistinct(nodeIdxGen, rapid.ID).Draw(t, "roots")
	edges := rapid.MapOf(nodeValueGen, rapid.SliceOfDistinct(nodeIdxGen, rapid.ID)).
		Draw(t, "edges")

	g := Graph[string]{
		Roots:  roots,
		Values: allNodes,
		View:   func(n string) string { return n },
		Edges:  func(n string) []int { return edges[n] },
	}

	var out strings.Builder
	if err := Write(&out, g, Options[string]{
		Style: plainStyle(),
	}); err != nil {
		t.Skip(err)
	}
	t.Logf("output:\n%s", out.String())
	lines := strings.Split(out.String(), "\n")

	// Verify that all box drawing characters with attachment points
	// are connected to other box drawing characters.
	for lineIdx, lineStr := range lines {
		lineNo := lineIdx + 1

		line := []rune(lineStr)
		for runeIdx, lineRune := range line {
			colNo := runeIdx + 1
			r := boxRune(lineRune)
			if !r.Valid() {
				// Not a box drawing character.
				continue
			}

			// Expect rune on the left with a right attachment.
			if r.HasLeft() {
				if runeIdx == 0 {
					t.Errorf("%d:%d:joint %q wants left attachment, got: start of line", lineNo, colNo, r)
				}

				left := boxRune(line[runeIdx-1])
				if !left.Valid() || !left.HasRight() {
					t.Errorf("%d:%d:joint %q wants left attachment, got: %q", lineNo, colNo, r, left)
				}
			}

			// Expect a rune on the right with a left attachment
			// OR a □ character.
			if r.HasRight() {
				if runeIdx == len(line)-1 {
					t.Errorf("%d:%d:joint %q wants right attachment, got: end of line", lineNo, colNo, r)
				}

				right := boxRune(line[runeIdx+1])
				ok := right.Valid() && right.HasLeft()

				// If it's not a box drawing character, it must be a □.
				ok = ok || right == '□'

				if !ok {
					t.Errorf("%d:%d:joint %q wants right attachment or '□', got: %q", lineNo, colNo, r, right)
				}
			}

			// Expect a rune above with a down attachment.
			if r.HasUp() {
				if lineIdx == 0 {
					t.Errorf("%d:%d:joint %q wants attachment above, got: start of file", lineNo, colNo, r)
				}

				lineAbove := []rune(lines[lineIdx-1])
				if len(lineAbove) <= runeIdx {
					t.Errorf("%d:%d:joint %q wants attachment above, got: line too short", lineNo, colNo, r)
				}

				above := boxRune(lineAbove[runeIdx])
				if !above.Valid() || !above.HasDown() {
					t.Errorf("%d:%d:joint %q wants attachment above, got: %q", lineNo, colNo, r, above)
				}
			}

			// Expect a rune below with a top attachment
			// OR if this is the second to last line,
			// a printable character.
			if r.HasDown() {
				if lineIdx == len(lines)-1 {
					t.Errorf("%d:%d:joint %q wants attachment below, got: end of file", lineNo, colNo, r)
				}

				lineBelow := []rune(lines[lineIdx+1])
				if len(lineBelow) <= runeIdx {
					t.Errorf("%d:%d:joint %q wants attachment below, got: line too short", lineNo, colNo, r)
				}

				below := boxRune(lineBelow[runeIdx])
				ok := below.Valid() || !below.HasUp()

				// If it's not a box character,
				// this must be the second to last line
				// and it must be a printable character.
				if lineIdx+1 == len(lines)-1 {
					if unicode.IsPrint(rune(below)) {
						ok = true
					}
				}

				if !ok {
					t.Errorf("%d:%d:joint %q wants attachment below, got: %q", lineNo, colNo, r, below)
				}

			}

		}
	}
}

func stripTrailingSpaces(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

package spice

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"pgregory.net/rapid"
)

func TestBranchGraph(t *testing.T) {
	// A branch graph with the following structure:
	//
	//	main ---> feature1 --> {feature2, feature4}
	//	      '-> feature3 --> feature5
	graph, err := NewBranchGraph(t.Context(), &branchLoaderStub{
		trunk: "main",
		branches: []LoadBranchItem{
			{Name: "feature1", Base: "main"},
			{Name: "feature2", Base: "feature1"},
			{Name: "feature4", Base: "feature1"},
			{Name: "feature3", Base: "main"},
			{Name: "feature5", Base: "feature3"},
		},
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, "main", graph.Trunk())

	t.Run("Aboves", func(t *testing.T) {
		tests := []struct {
			name string
			want []string
		}{
			{name: "main", want: []string{"feature1", "feature3"}},
			{name: "feature1", want: []string{"feature2", "feature4"}},
			{name: "feature2"},
			{name: "feature3", want: []string{"feature5"}},
			{name: "feature5"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				aboves := slices.Collect(graph.Aboves(tt.name))
				assert.Equal(t, tt.want, aboves)
			})
		}
	})

	t.Run("Upstack", func(t *testing.T) {
		tests := []struct {
			name string
			want []string
		}{
			{
				name: "main",
				want: []string{
					"main",
					"feature1", "feature3",
					"feature2", "feature4",
					"feature5",
				},
			},
			{
				name: "feature1",
				want: []string{"feature1", "feature2", "feature4"},
			},
			{name: "feature3", want: []string{"feature3", "feature5"}},
			{name: "feature2", want: []string{"feature2"}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				upstack := slices.Collect(graph.Upstack(tt.name))
				assert.Equal(t, tt.want, upstack)
			})
		}
	})

	t.Run("Tops", func(t *testing.T) {
		tests := []struct {
			name string
			want []string
		}{
			{name: "main", want: []string{"feature2", "feature4", "feature5"}},
			{name: "feature1", want: []string{"feature2", "feature4"}},
			{name: "feature2", want: []string{"feature2"}},
			{name: "feature3", want: []string{"feature5"}},
			{name: "feature5", want: []string{"feature5"}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tops := slices.Collect(graph.Tops(tt.name))
				assert.Equal(t, tt.want, tops)
			})
		}
	})

	t.Run("Downstack", func(t *testing.T) {
		tests := []struct {
			name string
			want []string
		}{
			{name: "main"},
			{name: "feature1", want: []string{"feature1"}},
			{name: "feature2", want: []string{"feature2", "feature1"}},
			{name: "feature3", want: []string{"feature3"}},
			{name: "feature5", want: []string{"feature5", "feature3"}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				downstack := slices.Collect(graph.Downstack(tt.name))
				assert.Equal(t, tt.want, downstack)
			})
		}
	})

	t.Run("Bottom", func(t *testing.T) {
		tests := []struct {
			name string
			want string
		}{
			{name: "main"},
			{name: "feature1", want: "feature1"},
			{name: "feature2", want: "feature1"},
			{name: "feature3", want: "feature3"},
			{name: "feature4", want: "feature1"},
			{name: "feature5", want: "feature3"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				bottom := graph.Bottom(tt.name)
				assert.Equal(t, tt.want, bottom)
			})
		}
	})

	t.Run("Stack", func(t *testing.T) {
		tests := []struct {
			name string
			want []string
		}{
			{
				name: "main",
				want: []string{
					"main",
					"feature1", "feature3",
					"feature2", "feature4",
					"feature5",
				},
			},
			{
				name: "feature1",
				want: []string{"feature1", "feature2", "feature4"},
			},
			{
				name: "feature2",
				want: []string{"feature1", "feature2"},
			},
			{
				name: "feature3",
				want: []string{"feature3", "feature5"},
			},
			{
				name: "feature5",
				want: []string{"feature3", "feature5"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stack := slices.Collect(graph.Stack(tt.name))
				assert.Equal(t, tt.want, stack)
			})
		}
	})
}

func TestBranchGraphRapid(t *testing.T) {
	rapid.Check(t, testBranchGraphRapid)
}

func testBranchGraphRapid(t *rapid.T) {
	branchNameGen := rapid.StringMatching(`[a-zA-Z0-9]{1,}`)
	gitHashGen := rapid.StringOfN(rapid.RuneFrom([]rune("0123456789abcdef")), 40, 40, 40)

	trunk := branchNameGen.Draw(t, "trunk")
	allBranches := []string{trunk}

	// To guarantee no cycles in generated graph,
	// we'll only use previously generated branches as bases.
	var branchItems []LoadBranchItem
	for range rapid.IntRange(0, 100).Draw(t, "numBranches") {
		name := branchNameGen.Filter(func(name string) bool {
			// Requested name must be unused.
			return !slices.Contains(allBranches, name)
		}).Draw(t, "branchName")
		base := rapid.SampledFrom(allBranches).Draw(t, "baseBranch")
		allBranches = append(allBranches, name)
		branchItems = append(branchItems, LoadBranchItem{
			Name:           name,
			Base:           base,
			Head:           git.Hash(gitHashGen.Draw(t, "headHash")),
			BaseHash:       git.Hash(gitHashGen.Draw(t, "baseHash")),
			UpstreamBranch: name,
		})
	}

	graph, err := NewBranchGraph(t.Context(), &branchLoaderStub{
		trunk:    trunk,
		branches: branchItems,
	}, nil)
	require.NoError(t, err)

	t.Repeat(map[string]func(*rapid.T){
		"Aboves": func(t *rapid.T) {
			branch := rapid.SampledFrom(allBranches).Draw(t, "branch")
			_ = slices.Collect(graph.Aboves(branch))
			// no validation, just don't panic
		},
		"Upstack": func(t *rapid.T) {
			branch := rapid.SampledFrom(allBranches).Draw(t, "branch")
			upstack := slices.Collect(graph.Upstack(branch))
			if assert.NotEmpty(t, upstack, "upstack should not be empty for tracked branches") {
				assert.Equal(t, branch, upstack[0], "upstack should start with the branch itself")
			}
		},
		"Tops": func(t *rapid.T) {
			branch := rapid.SampledFrom(allBranches).Draw(t, "branch")
			tops := slices.Collect(graph.Tops(branch))
			assert.NotEmpty(t, tops, "tops should not be empty for any branch")
			for _, top := range tops {
				// There must be no branches above the top.
				upstack := slices.Collect(graph.Upstack(top))[1:] // skip top
				assert.Empty(t, upstack, "top branch should not have any branches above it")
			}
		},
		"Downstack": func(t *rapid.T) {
			branch := rapid.SampledFrom(allBranches).Draw(t, "branch")
			downstack := slices.Collect(graph.Downstack(branch))
			assert.NotContains(t, downstack, trunk, "downstack should not contain trunk branch")
			if branch == trunk {
				assert.Empty(t, downstack, "downstack should be empty for trunk branch")
			} else if assert.NotEmpty(t, downstack, "downstack should not be empty for tracked branches") {
				assert.Equal(t, branch, downstack[0], "downstack should start with the branch itself")
			}
		},
		"Bottom": func(t *rapid.T) {
			branch := rapid.SampledFrom(allBranches).Draw(t, "branch")
			bottom := graph.Bottom(branch)
			if branch == trunk {
				assert.Empty(t, bottom, "bottom should be empty for trunk branch")
			} else {
				assert.NotEmpty(t, bottom, "bottom should not be empty for tracked branches")
				assert.NotEqual(t, trunk, bottom, "bottom should not be trunk for tracked branches")

				// TODO: don't look at internals
				assert.Equal(t, trunk, graph.branches[graph.byName[bottom]].Base,
					"base of bottom branch should be trunk")
			}
		},
		"Stack": func(t *rapid.T) {
			branch := rapid.SampledFrom(allBranches).Draw(t, "branch")
			stack := slices.Collect(graph.Stack(branch))
			if assert.NotEmpty(t, stack, "stack should not be empty for tracked branches") {
				assert.Contains(t, stack, branch, "stack should contain the branch itself")
			}
		},
		"StackLinear": func(t *rapid.T) {
			branch := rapid.SampledFrom(allBranches).Draw(t, "branch")
			stack, err := graph.StackLinear(branch)
			if err != nil {
				// That's okay, the stack isn't linear.
				return
			}

			if assert.NotEmpty(t, stack, "linear stack should not be empty for tracked branches") {
				assert.Contains(t, stack, branch, "linear stack should contain the branch itself")
			}
		},
	})
}

type branchLoaderStub struct {
	trunk    string
	branches []LoadBranchItem
}

var _ BranchLoader = (*branchLoaderStub)(nil)

func (s *branchLoaderStub) Trunk() string {
	return s.trunk
}

func (s *branchLoaderStub) LoadBranches(_ context.Context) ([]LoadBranchItem, error) {
	return slices.Clone(s.branches), nil
}

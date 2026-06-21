package spice_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/spicetest"
	"pgregory.net/rapid"
)

func TestBranchGraph(t *testing.T) {
	// A branch graph with the following structure:
	//
	//	main ---> feature1 --> {feature2, feature4}
	//	      '-> feature3 --> feature5
	feature1WT := t.TempDir()
	feature5WT := t.TempDir()
	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
		Trunk: "main",
		Branches: []spice.LoadBranchItem{
			{Name: "feature1", Base: "main"},
			{Name: "feature2", Base: "feature1"},
			{Name: "feature4", Base: "feature1"},
			{Name: "feature3", Base: "main"},
			{Name: "feature5", Base: "feature3"},
		},
		Worktrees: map[string]string{
			"feature1": feature1WT,
			"feature5": feature5WT,
		},
	})

	assert.Equal(t, "main", graph.Trunk())

	t.Run("All", func(t *testing.T) {
		var gotNames []string
		for item := range graph.All() {
			gotNames = append(gotNames, item.Name)
		}
		assert.ElementsMatch(t, []string{
			"feature1", "feature2", "feature3", "feature4", "feature5",
		}, gotNames)
	})

	t.Run("Lookup", func(t *testing.T) {
		tests := []struct {
			name string
			ok   bool
		}{
			{"main", false},
			{"feature1", true},
			{"feature4", true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, ok := graph.Lookup(tt.name)
				assert.Equal(t, tt.ok, ok)
			})
		}
	})

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

	t.Run("NextBase", func(t *testing.T) {
		tests := []struct {
			name   string
			except []string
			want   string
		}{
			{name: "main", except: []string{"feature1"}, want: "main"},
			{name: "unknown", except: []string{"feature1"}, want: "main"},
			{name: "feature1", except: []string{"feature1"}, want: "main"},
			{name: "feature2", except: []string{"feature1"}, want: "main"},
			{name: "feature4", except: []string{"feature1"}, want: "main"},
			{name: "feature5", except: []string{"feature3"}, want: "main"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := graph.NextBase(tt.name, func(branch string) bool {
					return slices.Contains(tt.except, branch)
				})
				assert.Equal(t, tt.want, got)
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

	t.Run("Worktree", func(t *testing.T) {
		tests := []struct {
			name string
			want string
		}{
			{"main", ""},
			{"feature1", feature1WT},
			{"feature5", feature5WT},
			{"feature2", ""},
			{"feature3", ""},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := graph.Worktree(tt.name)
				assert.Equal(t, tt.want, got)
			})
		}
	})
}

func TestBranchGraph_NextBase_cycle(t *testing.T) {
	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
		Trunk: "main",
		Branches: []spice.LoadBranchItem{
			{Name: "feature1", Base: "feature2"},
			{Name: "feature2", Base: "feature1"},
		},
	})

	got := graph.NextBase("feature1", func(string) bool { return true })
	assert.Equal(t, "main", got)
}

func TestBranchGraph_NextBase_partialGraph(t *testing.T) {
	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
		Trunk: "main",
		Branches: []spice.LoadBranchItem{
			{Name: "feature1", Base: "feature2"},
			{Name: "feature2", Base: "survivor"},
		},
	})

	got := graph.NextBase("feature1", func(branch string) bool {
		return branch == "feature2"
	})
	assert.Equal(t, "survivor", got)
}

func TestBranchGraph_anchorIsRoot(t *testing.T) {
	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
		Trunk:   "main",
		Anchors: []string{"wt-a"},
		Branches: []spice.LoadBranchItem{
			{Name: "feat1", Base: "wt-a"},
			{Name: "feat2", Base: "feat1"},
			{Name: "other", Base: "main"},
		},
	})

	// Traversals terminate at the worktree trunk just like the canonical
	// trunk: the stack rooted at wt-a does not bleed into main.
	assert.Equal(t, "feat1", graph.Bottom("feat2"))
	assert.Equal(t, []string{"feat2", "feat1"},
		slices.Collect(graph.Downstack("feat2")))
	assert.Equal(t, "other", graph.Bottom("other"))
	assert.Empty(t, slices.Collect(graph.Downstack("wt-a")))

	// Anchors are reported as graph roots, excluding the canonical trunk.
	assert.Equal(t, []string{"wt-a"}, slices.Collect(graph.Anchors()))
}

// An internal anchor (one based on a branch owned by another worktree)
// severs the upstack: an operation scoped to the lower worktree's region
// must not reach into the upper worktree's region.
//
//	main ---> wt-a ---> feat1 ---> wt-b ---> featb1
//
// wt-a and wt-b are anchors. feat1 belongs to worktree A;
// featb1 belongs to worktree B, whose anchor wt-b is based on feat1.
func TestBranchGraph_internalAnchorSeversUpstack(t *testing.T) {
	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
		Trunk:   "main",
		Anchors: []string{"wt-a", "wt-b"},
		Branches: []spice.LoadBranchItem{
			{Name: "feat1", Base: "wt-a"},
			{Name: "featb1", Base: "wt-b"},
		},
	})

	// From worktree A's region, the upstack stops at feat1:
	// it does not bleed across the wt-b anchor into featb1.
	assert.Equal(t, []string{"feat1"}, slices.Collect(graph.Upstack("feat1")))
	assert.Empty(t, slices.Collect(graph.Aboves("feat1")))

	// Worktree B's region roots at its own anchor.
	assert.Equal(t, "featb1", graph.Bottom("featb1"))
	assert.Equal(t, []string{"featb1"}, slices.Collect(graph.Upstack("featb1")))
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
	var branchItems []spice.LoadBranchItem
	for range rapid.IntRange(0, 100).Draw(t, "numBranches") {
		name := branchNameGen.Filter(func(name string) bool {
			// Requested name must be unused.
			return !slices.Contains(allBranches, name)
		}).Draw(t, "branchName")
		base := rapid.SampledFrom(allBranches).Draw(t, "baseBranch")
		allBranches = append(allBranches, name)
		branchItems = append(branchItems, spice.LoadBranchItem{
			Name:           name,
			Base:           base,
			Head:           git.Hash(gitHashGen.Draw(t, "headHash")),
			BaseHash:       git.Hash(gitHashGen.Draw(t, "baseHash")),
			UpstreamBranch: name,
		})
	}

	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
		Trunk:    trunk,
		Branches: branchItems,
	})

	t.Repeat(map[string]func(*rapid.T){
		"All": func(t *rapid.T) {
			var gotNames []string
			for item := range graph.All() {
				gotNames = append(gotNames, item.Name)
			}

			assert.NotContains(t, gotNames, trunk,
				"trunk must not be listed as a tracked branch")
			gotNames = append(gotNames, trunk)

			assert.ElementsMatch(t, allBranches, gotNames)
		},
		"Lookup": func(t *rapid.T) {
			branch := rapid.SampledFrom(allBranches).Draw(t, "branch")
			got, ok := graph.Lookup(branch)
			if branch == trunk {
				assert.False(t, ok, "trunk should not be tracked")
			} else {
				assert.True(t, ok, "branch %q should be tracked", branch)
				assert.Equal(t, branch, got.Name)
			}
		},
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

				bottomBranch, ok := graph.Lookup(bottom)
				require.True(t, ok, "bottom branch should be tracked")
				assert.Equal(t, trunk, bottomBranch.Base,
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

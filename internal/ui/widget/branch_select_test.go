package widget

import (
	json "encoding/json/v2"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rogpeppe/go-internal/testscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/uitest"
)

// Runs tests inside testdata/script/branch_select.
// The following files are expected:
//
//   - want: name of the branch expected to be selected at the end
//   - branches: branches available in the list. See below for format.
//   - selected (optional): preselected branch name
//   - desc (optional): prompt description
//   - home (optional): user's home directory
//   - worktree (optional): current worktree
//
// The branches file is a JSON-encoded file with the format:
//
//	[
//		{branch: string, base: string?, disabled: bool?},
//		...
//	]
func TestBranchTreeSelect_Script(t *testing.T) {
	uitest.RunScripts(t,
		func(t testing.TB, ts *testscript.TestScript, view ui.InteractiveView) {
			wantBranch := strings.TrimSpace(ts.ReadFile("want"))

			var input []BranchTreeItem
			require.NoError(t,
				json.Unmarshal(
					[]byte(ts.ReadFile("branches")),
					&input,
					json.MatchCaseInsensitiveNames(true),
				),
				"read 'branches' file")

			desc := readOptionalFile(ts, "desc")

			if home := readOptionalFile(ts, "home"); home != "" {
				ts.Setenv("HOME", home)
			}

			worktree := readOptionalFile(ts, "worktree")

			gotBranch := readOptionalFile(ts, "selected")
			widget := NewBranchTreeSelect().
				WithTitle("Select a branch").
				WithItems(input...).
				WithDescription(desc).
				WithValue(&gotBranch).
				WithCurrentWorktree(worktree)

			assert.NoError(t, ui.Run(view, widget))
			assert.Equal(t, wantBranch, gotBranch)
		},
		&uitest.RunScriptsOptions{
			Update: *UpdateFixtures,
		},
		"testdata/script/branch_tree_select",
	)
}

func TestBranchTreeSelect_Update_ranking(t *testing.T) {
	tests := []struct {
		name         string
		giveItems    []BranchTreeItem
		giveSelected string
		giveFilter   string
		want         string
	}{
		{
			name: "CaseInsensitive",
			giveItems: []BranchTreeItem{
				{Branch: "MAIN"},
				{Branch: "main-feature", Base: "MAIN"},
			},
			giveSelected: "main-feature",
			giveFilter:   "MaIn",
			want:         "MAIN",
		},
		{
			name: "UnicodeCaseInsensitive",
			giveItems: []BranchTreeItem{
				{Branch: "MÄIN"},
				{Branch: "mäin-feature", Base: "MÄIN"},
			},
			giveSelected: "mäin-feature",
			giveFilter:   "mäin",
			want:         "MÄIN",
		},
		{
			name: "DisabledExactMatch",
			giveItems: []BranchTreeItem{
				{Branch: "main", Disabled: true},
				{Branch: "main-feature", Base: "main"},
			},
			giveSelected: "main-feature",
			giveFilter:   "main",
			want:         "main-feature",
		},
		{
			name: "UnselectableBase",
			giveItems: []BranchTreeItem{
				{Branch: "main-feature", Base: "main"},
			},
			giveSelected: "main-feature",
			giveFilter:   "main",
			want:         "main-feature",
		},
		{
			name: "BetterPartialMatch",
			giveItems: []BranchTreeItem{
				{Branch: "main"},
				{Branch: "main-feature", Base: "main"},
			},
			giveSelected: "main-feature",
			giveFilter:   "mai",
			want:         "main",
		},
		{
			name: "ExactNameOverridesChangeID",
			giveItems: []BranchTreeItem{
				{Branch: "123"},
				{Branch: "feature", Base: "123", ChangeID: "#123"},
			},
			giveSelected: "feature",
			giveFilter:   "123",
			want:         "123",
		},
		{
			name: "ExactNameOverridesWorktree",
			giveItems: []BranchTreeItem{
				{Branch: "main"},
				{Branch: "feature", Base: "main", Worktree: "/tmp/main"},
			},
			giveSelected: "feature",
			giveFilter:   "main",
			want:         "main",
		},
		{
			name: "BranchNameOutranksWorktreeOnlyMatch",
			giveItems: []BranchTreeItem{
				{Branch: "main-feature"},
				{Branch: "feature", Worktree: "/tmp/main"},
			},
			giveSelected: "feature",
			giveFilter:   "main",
			want:         "main-feature",
		},
		{
			name: "BranchNameOutranksChangeIDOnlyMatch",
			giveItems: []BranchTreeItem{
				{Branch: "123-feature"},
				{Branch: "feature", ChangeID: "#123"},
			},
			giveSelected: "feature",
			giveFilter:   "123",
			want:         "123-feature",
		},
		{
			name: "EqualScoresKeepFocus",
			giveItems: []BranchTreeItem{
				{Branch: "main-one"},
				{Branch: "main-two"},
			},
			giveSelected: "main-two",
			giveFilter:   "mai",
			want:         "main-two",
		},
		{
			name: "LowerScoreTieDoesNotKeepFocus",
			giveItems: []BranchTreeItem{
				{Branch: "main"},
				{Branch: "main-one"},
				{Branch: "main-two"},
			},
			giveSelected: "main-two",
			giveFilter:   "mai",
			want:         "main",
		},
		{
			name: "EqualScoresUseTreeOrder",
			giveItems: []BranchTreeItem{
				{Branch: "main-one"},
				{Branch: "main-two"},
				{Branch: "feature"},
			},
			giveSelected: "feature",
			giveFilter:   "mai",
			want:         "main-one",
		},
		{
			name: "HigherScoreOutranksFullName",
			giveItems: []BranchTreeItem{
				{Branch: "main"},
				{Branch: "mAiN-extra"},
			},
			giveSelected: "main",
			giveFilter:   "main",
			want:         "mAiN-extra",
		},
		{
			name: "MetadataOnlyMatchesKeepFocus",
			giveItems: []BranchTreeItem{
				{Branch: "feature-one", ChangeID: "#123"},
				{Branch: "feature-two", Worktree: "/tmp/123"},
			},
			giveSelected: "feature-two",
			giveFilter:   "123",
			want:         "feature-two",
		},
		{
			name: "CaseVariantsKeepFocus",
			giveItems: []BranchTreeItem{
				{Branch: "main"},
				{Branch: "MAIN"},
			},
			giveSelected: "MAIN",
			giveFilter:   "main",
			want:         "MAIN",
		},
		{
			name: "CaseVariantsUseTreeOrder",
			giveItems: []BranchTreeItem{
				{Branch: "main"},
				{Branch: "MAIN"},
				{Branch: "main-feature"},
			},
			giveSelected: "main-feature",
			giveFilter:   "main",
			want:         "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selected := tt.giveSelected
			widget := NewBranchTreeSelect().
				WithItems(tt.giveItems...).
				WithValue(&selected)
			widget.Init()

			for _, r := range tt.giveFilter {
				widget.Update(tea.KeyPressMsg{Text: string(r), Code: r})
			}

			require.NoError(t, widget.Err())
			require.NotNil(t, widget.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
			assert.Equal(t, tt.want, widget.Value())
		})
	}
}

func TestBranchTreeSelect_Update_filterNavigation(t *testing.T) {
	tests := []struct {
		name     string
		giveKeys []tea.KeyPressMsg
		want     string
	}{
		{
			name: "EmptyFilterKeepsFocus",
			want: "main-feature",
		},
		{
			name: "ArrowAfterExactMatch",
			giveKeys: []tea.KeyPressMsg{
				{Text: "main"},
				{Code: tea.KeyDown},
			},
			want: "main-feature",
		},
		{
			name: "FilterEditReranksManualSelection",
			giveKeys: []tea.KeyPressMsg{
				{Text: "main"},
				{Code: tea.KeyDown},
				{Code: tea.KeyBackspace},
			},
			want: "main",
		},
		{
			name: "ArrowAfterPartialMatch",
			giveKeys: []tea.KeyPressMsg{
				{Text: "mai"},
				{Code: tea.KeyDown},
			},
			want: "main-feature",
		},
		{
			name: "BackspaceToPartialMatch",
			giveKeys: []tea.KeyPressMsg{
				{Text: "main"},
				{Code: tea.KeyBackspace},
			},
			want: "main",
		},
		{
			name: "ClearAfterManualSelection",
			giveKeys: []tea.KeyPressMsg{
				{Text: "main"},
				{Code: tea.KeyDown},
				{Code: 'u', Mod: tea.ModCtrl},
			},
			want: "main-feature",
		},
		{
			name: "ExtendBeyondExactMatch",
			giveKeys: []tea.KeyPressMsg{
				{Text: "main"},
				{Text: "-f"},
			},
			want: "main-feature",
		},
		{
			name: "BackspaceToExactMatch",
			giveKeys: []tea.KeyPressMsg{
				{Text: "main-f"},
				{Code: tea.KeyBackspace},
				{Code: tea.KeyBackspace},
			},
			want: "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selected := "main-feature"
			widget := NewBranchTreeSelect().
				WithItems(
					BranchTreeItem{Branch: "main"},
					BranchTreeItem{Branch: "main-feature", Base: "main"},
				).
				WithValue(&selected)
			widget.Init()

			for _, msg := range tt.giveKeys {
				widget.Update(msg)
			}

			require.NoError(t, widget.Err())
			require.NotNil(t, widget.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
			assert.Equal(t, tt.want, widget.Value())
		})
	}
}

func readOptionalFile(ts *testscript.TestScript, path string) string {
	if _, err := os.Stat(ts.MkAbs(path)); err == nil {
		return strings.TrimSpace(ts.ReadFile(path))
	}
	return ""
}

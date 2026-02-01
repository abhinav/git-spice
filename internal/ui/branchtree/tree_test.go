package branchtree

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/commit"
	"go.abhg.dev/testing/stub"
)

func TestWrite(t *testing.T) {
	p := filepath.FromSlash

	// Fixed time for consistent commit rendering.
	now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		give Graph
		opts *GraphOptions
		want string
	}{
		{
			name: "SingleBranch",
			give: Graph{
				Items: []*Item{{Branch: "main"}},
				Roots: []int{0},
			},
			want: "main\n",
		},
		{
			name: "LinearChain",
			give: Graph{
				Items: []*Item{
					{Branch: "main", Aboves: []int{1}},
					{Branch: "feat1", Aboves: []int{2}},
					{Branch: "feat2"},
				},
				Roots: []int{0},
			},
			want: joinLines(
				"  ┏━□ feat2",
				"┏━┻□ feat1",
				"main",
			),
		},
		{
			name: "MultipleChildren",
			give: Graph{
				Items: []*Item{
					{Branch: "main", Aboves: []int{1, 2}},
					{Branch: "feat1"},
					{Branch: "feat2"},
				},
				Roots: []int{0},
			},
			want: joinLines(
				"┏━□ feat1",
				"┣━□ feat2",
				"main",
			),
		},
		{
			name: "DeepTree",
			give: Graph{
				Items: []*Item{
					{Branch: "main", Aboves: []int{1, 3}},
					{Branch: "feat1", Aboves: []int{2}},
					{Branch: "feat1.1"},
					{Branch: "feat2", Aboves: []int{4}},
					{Branch: "feat2.1"},
				},
				Roots: []int{0},
			},
			want: joinLines(
				"  ┏━□ feat1.1",
				"┏━┻□ feat1",
				"┃ ┏━□ feat2.1",
				"┣━┻□ feat2",
				"main",
			),
		},
		{
			name: "MultipleTrees",
			give: Graph{
				Items: []*Item{
					{Branch: "main", Aboves: []int{1}},
					{Branch: "feat1"},
					{Branch: "develop", Aboves: []int{3}},
					{Branch: "hotfix"},
				},
				Roots: []int{0, 2},
			},
			want: joinLines(
				"┏━□ feat1",
				"main",
				"┏━□ hotfix",
				"develop",
			),
		},
		{
			name: "WithChangeID",
			give: Graph{
				Items: []*Item{{Branch: "feat1", ChangeID: "#123"}},
				Roots: []int{0},
			},
			want: "feat1 (#123)\n",
		},
		{
			name: "WithChangeStateOpen",
			give: Graph{
				Items: []*Item{{
					Branch:      "feat1",
					ChangeID:    "#123",
					ChangeState: ptr(forge.ChangeOpen),
				}},
				Roots: []int{0},
			},
			want: "feat1 (#123 open)\n",
		},
		{
			name: "WithChangeStateClosed",
			give: Graph{
				Items: []*Item{{
					Branch:      "feat1",
					ChangeID:    "#456",
					ChangeState: ptr(forge.ChangeClosed),
				}},
				Roots: []int{0},
			},
			want: "feat1 (#456 closed)\n",
		},
		{
			name: "WithChangeStateMerged",
			give: Graph{
				Items: []*Item{{
					Branch:      "feat1",
					ChangeID:    "#789",
					ChangeState: ptr(forge.ChangeMerged),
				}},
				Roots: []int{0},
			},
			want: "feat1 (#789 merged)\n",
		},
		{
			name: "WithWorktree",
			give: Graph{
				Items: []*Item{{Branch: "feat1", Worktree: p("/path/to/worktree")}},
				Roots: []int{0},
			},
			want: "feat1 [wt: " + p("/path/to/worktree") + "]\n",
		},
		{
			name: "NeedsRestack",
			give: Graph{
				Items: []*Item{{Branch: "feat1", NeedsRestack: true}},
				Roots: []int{0},
			},
			want: "feat1 (needs restack)\n",
		},
		{
			name: "Highlighted",
			give: Graph{
				Items: []*Item{{Branch: "feat1", Highlighted: true}},
				Roots: []int{0},
			},
			want: "feat1 ◀\n",
		},
		{
			name: "Disabled",
			give: Graph{
				Items: []*Item{{Branch: "feat1", Disabled: true}},
				Roots: []int{0},
			},
			want: "feat1\n",
		},
		{
			name: "WithCommits",
			give: Graph{
				Items: []*Item{{
					Branch: "feat1",
					Commits: []commit.Summary{
						{ShortHash: "abc1234", Subject: "Add feature", AuthorDate: now.Add(-2 * time.Hour)},
						{ShortHash: "def5678", Subject: "Fix bug", AuthorDate: now.Add(-1 * time.Hour)},
					},
				}},
				Roots: []int{0},
			},
			opts: &GraphOptions{
				CommitStyle: plainCommitStyle(),
			},
			want: joinLines(
				"feat1",
				"abc1234 Add feature (2 years ago)",
				"def5678 Fix bug (2 years ago)",
			),
		},
		{
			name: "PushStatusSimple",
			give: Graph{
				Items: []*Item{{
					Branch:     "feat1",
					PushStatus: PushStatus{NeedsPush: true},
				}},
				Roots: []int{0},
			},
			opts: &GraphOptions{
				PushStatusFormat: PushStatusSimple,
			},
			want: "feat1 (needs push)\n",
		},
		{
			name: "PushStatusAheadOnly",
			give: Graph{
				Items: []*Item{{
					Branch:     "feat1",
					PushStatus: PushStatus{Ahead: 3},
				}},
				Roots: []int{0},
			},
			opts: &GraphOptions{
				PushStatusFormat: PushStatusAheadBehind,
			},
			want: "feat1 (⇡3)\n",
		},
		{
			name: "PushStatusBehindOnly",
			give: Graph{
				Items: []*Item{{
					Branch:     "feat1",
					PushStatus: PushStatus{Behind: 2},
				}},
				Roots: []int{0},
			},
			opts: &GraphOptions{
				PushStatusFormat: PushStatusAheadBehind,
			},
			want: "feat1 (⇣2)\n",
		},
		{
			name: "PushStatusAheadBehind",
			give: Graph{
				Items: []*Item{{
					Branch:     "feat1",
					PushStatus: PushStatus{Ahead: 3, Behind: 2},
				}},
				Roots: []int{0},
			},
			opts: &GraphOptions{
				PushStatusFormat: PushStatusAheadBehind,
			},
			want: "feat1 (⇡3⇣2)\n",
		},
		{
			name: "CurrentWorktreeHidden",
			give: Graph{
				Items: []*Item{{Branch: "feat1", Worktree: p("/current/worktree")}},
				Roots: []int{0},
			},
			opts: &GraphOptions{
				CurrentWorktree: p("/current/worktree"),
			},
			want: "feat1\n",
		},
		{
			name: "WorktreeWithHomeSubstitution",
			give: Graph{
				Items: []*Item{{Branch: "feat1", Worktree: p("/home/user/projects/repo")}},
				Roots: []int{0},
			},
			opts: &GraphOptions{
				HomeDir: p("/home/user"),
			},
			want: "feat1 [wt: " + p("~/projects/repo") + "]\n",
		},
		{
			name: "AllFeatures",
			give: Graph{
				Items: []*Item{{
					Branch:       "feat1",
					ChangeID:     "#42",
					ChangeState:  ptr(forge.ChangeOpen),
					Worktree:     p("/home/user/feat1"),
					NeedsRestack: true,
					PushStatus:   PushStatus{Ahead: 1},
					Highlighted:  true,
					Commits: []commit.Summary{
						{ShortHash: "abc1234", Subject: "WIP", AuthorDate: now.Add(-30 * time.Minute)},
					},
				}},
				Roots: []int{0},
			},
			opts: &GraphOptions{
				HomeDir:          p("/home/user"),
				PushStatusFormat: PushStatusAheadBehind,
				CommitStyle:      plainCommitStyle(),
			},
			want: joinLines(
				"feat1 (#42 open) [wt: "+p("~/feat1")+"] (needs restack) (⇡1) ◀",
				"abc1234 WIP (2 years ago)",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts
			if opts == nil {
				opts = &GraphOptions{}
			}
			opts.Style = plainStyle()
			if opts.CommitStyle == nil {
				opts.CommitStyle = plainCommitStyle()
			}

			var sb strings.Builder
			require.NoError(t, Write(&sb, tt.give, opts))
			assert.Equal(t, tt.want, sb.String())
		})
	}
}

func TestBranchTreeRenderer_worktree(t *testing.T) {
	p := filepath.FromSlash

	tests := []struct {
		name    string
		wt      string
		homeDir string
		want    string
	}{
		{
			name: "NoHomeDir",
			wt:   p("/path/to/worktree"),
			want: " [wt: " + p("/path/to/worktree") + "]",
		},
		{
			name:    "WithHomeSubstitution",
			wt:      p("/home/user/projects/repo"),
			homeDir: p("/home/user"),
			want:    " [wt: " + p("~/projects/repo") + "]",
		},
		{
			name:    "PathOutsideHome",
			wt:      p("/opt/repos/myproject"),
			homeDir: p("/home/user"),
			want:    " [wt: " + p("/opt/repos/myproject") + "]",
		},
		{
			name:    "ExactlyHomeDir",
			wt:      p("/home/user"),
			homeDir: p("/home/user"),
			want:    " [wt: ~]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &branchTreeRenderer{
				Style:   *plainStyle(),
				HomeDir: tt.homeDir,
			}

			var sb strings.Builder
			r.worktree(&sb, tt.wt, nil)

			assert.Equal(t, tt.want, sb.String())
		})
	}
}

func TestBranchTreeRenderer_worktreeHighlights(t *testing.T) {
	p := filepath.FromSlash

	tests := []struct {
		name    string
		wt      highlightedString
		homeDir string
		want    highlightedString
	}{
		{
			name: "NoTransformation",
			wt:   highlightedString(p("/t{m}p/repo")),
			want: highlightedString(p("/t{m}p/repo")),
		},
		{
			name:    "NoHighlights",
			wt:      highlightedString(p("/home/user/projects")),
			homeDir: p("/home/user"),
			want:    highlightedString(p("~/projects")),
		},
		{
			name:    "HighlightsInHomePart",
			wt:      highlightedString(p("/h{o}m{e}/user/projects")),
			homeDir: p("/home/user"),
			want:    highlightedString(p("{~}/projects")),
		},
		{
			name:    "HighlightsInRelativePart",
			wt:      highlightedString(p("/home/user/{p}ro{j}ects")),
			homeDir: p("/home/user"),
			want:    highlightedString(p("~/{p}ro{j}ects")),
		},
		{
			name:    "MixedHighlights",
			wt:      highlightedString(p("/ho{m}e/user/{p}rojects")),
			homeDir: p("/home/user"),
			want:    highlightedString(p("{~}/{p}rojects")),
		},
		{
			name:    "MultipleHighlightsInHome",
			wt:      highlightedString(p("/{h}{o}{m}{e}/user/repo")),
			homeDir: p("/home/user"),
			want:    highlightedString(p("{~}/repo")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wt, highlights := tt.wt.Split()

			var capturedText string
			var capturedHighlights []int
			defer stub.Value(&renderTextWithHighlights,
				func(
					_ *strings.Builder,
					text string,
					hl []int,
					_, _ lipgloss.Style,
				) {
					capturedText = text
					capturedHighlights = hl
				},
			)()

			r := &branchTreeRenderer{
				Style:   *plainStyle(),
				HomeDir: tt.homeDir,
			}

			var sb strings.Builder
			r.worktree(&sb, wt, highlights)

			wantText, wantHighlights := tt.want.Split()
			assert.Equal(t, wantText, capturedText, "text mismatch")
			assert.Equal(t, wantHighlights, capturedHighlights, "highlights mismatch")
		})
	}
}

// highlightedString represents text with highlighted characters.
// Characters wrapped in {braces} are highlighted.
// Example: "fo{o}bar" represents "foobar" with index 2 highlighted.
type highlightedString string

// Split parses the highlighted string into plain text and highlight indexes.
func (s highlightedString) Split() (text string, highlights []int) {
	var sb strings.Builder
	var runeCount int
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '{' {
			// Find closing brace.
			j := i + 1
			for j < len(runes) && runes[j] != '}' {
				j++
			}
			// Add each character between braces as highlighted.
			for k := i + 1; k < j; k++ {
				highlights = append(highlights, runeCount)
				sb.WriteRune(runes[k])
				runeCount++
			}
			i = j // skip past '}'
			continue
		}
		sb.WriteRune(runes[i])
		runeCount++
	}
	return sb.String(), highlights
}

func TestHighlightedString_Split(t *testing.T) {
	tests := []struct {
		name           string
		give           highlightedString
		wantText       string
		wantHighlights []int
	}{
		{
			name:           "NoHighlights",
			give:           "foobar",
			wantText:       "foobar",
			wantHighlights: nil,
		},
		{
			name:           "FirstChar",
			give:           "{f}oobar",
			wantText:       "foobar",
			wantHighlights: []int{0},
		},
		{
			name:           "LastChar",
			give:           "fooba{r}",
			wantText:       "foobar",
			wantHighlights: []int{5},
		},
		{
			name:           "MiddleChar",
			give:           "fo{o}bar",
			wantText:       "foobar",
			wantHighlights: []int{2},
		},
		{
			name:           "MultipleChars",
			give:           "{f}o{o}ba{r}",
			wantText:       "foobar",
			wantHighlights: []int{0, 2, 5},
		},
		{
			name:           "ContiguousChars",
			give:           "fo{ob}ar",
			wantText:       "foobar",
			wantHighlights: []int{2, 3},
		},
		{
			name:           "Unicode",
			give:           "héllo {w}ör{l}d",
			wantText:       "héllo wörld",
			wantHighlights: []int{6, 9},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, highlights := tt.give.Split()
			assert.Equal(t, tt.wantText, text)
			assert.Equal(t, tt.wantHighlights, highlights)
		})
	}
}

// plainStyle returns a style with no ANSI escapes for readable test output.
func plainStyle() *Style {
	return &Style{
		Branch:            ui.NewStyle(),
		BranchHighlighted: ui.NewStyle(),
		ChangeID:          ui.NewStyle(),
		ChangeState: ChangeStateStyle{
			Open:   ui.NewStyle().SetString("open"),
			Closed: ui.NewStyle().SetString("closed"),
			Merged: ui.NewStyle().SetString("merged"),
		},
		Worktree:              ui.NewStyle(),
		PushStatus:            ui.NewStyle(),
		NeedsRestack:          ui.NewStyle().SetString(" (needs restack)"),
		NodeMarker:            ui.NewStyle().SetString("□"),
		NodeMarkerHighlighted: ui.NewStyle().SetString("■"),
		NodeMarkerDisabled:    ui.NewStyle().SetString("□"),
		TextHighlight:         ui.NewStyle(),
		Marker:                ui.NewStyle().SetString("◀"),
	}
}

func plainCommitStyle() *commit.SummaryStyle {
	return &commit.SummaryStyle{
		Hash:    ui.NewStyle(),
		Subject: ui.NewStyle(),
		Time:    ui.NewStyle(),
	}
}

// joinLines joins lines with newlines and adds a trailing newline.
func joinLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func ptr[T any](v T) *T {
	return &v
}

package widget

import (
	"encoding/json"
	"fmt"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/commit"
)

// BranchSplitStyle defines the styles for [BranchSplit].
type BranchSplitStyle struct {
	Commit     commit.SummaryStyle
	HeadCommit commit.SummaryStyle

	SplitMarker lipgloss.Style
	HeadMarker  lipgloss.Style
}

// DefaultBranchSplitStyle is the default style for a [BranchSplit].
var DefaultBranchSplitStyle = BranchSplitStyle{
	Commit:      commit.DefaultSummaryStyle,
	HeadCommit:  commit.DefaultSummaryStyle.Faint(true),
	SplitMarker: ui.NewStyle().SetString("□"),
	HeadMarker:  ui.NewStyle().SetString("■"),
}

// BranchSplit is a widget that allows users to pick out commits to split
// from the current branch.
//
// It displays a list of commits, the last of which is not selectable.
type BranchSplit struct {
	Style BranchSplitStyle

	model   *ui.MultiSelect[commit.Summary]
	commits []commit.Summary
	head    string
}

var _ ui.Field = (*BranchSplit)(nil)

// NewBranchSplit creates a new [BranchSplit] widget.
func NewBranchSplit() *BranchSplit {
	bs := &BranchSplit{
		Style: DefaultBranchSplitStyle,
	}
	bs.model = ui.NewMultiSelect(bs.renderCommit)
	bs.model.Style.ScrollUp = ui.NewStyle().Foreground(ui.Gray).SetString("    ▲▲▲")
	bs.model.Style.ScrollDown = ui.NewStyle().Foreground(ui.Gray).SetString("    ▼▼▼")
	return bs
}

// WithCommits sets the commits to be listed in a branch split widget.
func (b *BranchSplit) WithCommits(commits ...commit.Summary) *BranchSplit {
	must.Bef(len(commits) >= 2, "cannot split a branch with fewer than 2 commits")
	b.commits = commits
	return b
}

// Selected returns the indexes of the selected commits.
func (b *BranchSplit) Selected() []int {
	return b.model.Selected()
}

// UnmarshalValue reads a list of commit hashes
// for the selected commits.
func (b *BranchSplit) UnmarshalValue(unmarshal func(any) error) error {
	var commits []git.Hash
	if err := unmarshal(&commits); err != nil {
		return err
	}

	selected := make([]int, 0, len(commits))
	for _, c := range commits {
		idx := slices.IndexFunc(b.commits, func(summary commit.Summary) bool {
			return summary.ShortHash == c
		})
		if idx == -1 {
			return fmt.Errorf("cannot select %v: commit not found", c)
		}

		selected = append(selected, idx)
	}

	bs, err := json.Marshal(selected)
	if err != nil {
		return fmt.Errorf("marshal selected: %w", err)
	}

	return b.model.UnmarshalValue(func(a any) error {
		return json.Unmarshal(bs, a)
	})
}

// Title returns the title of the widget.
func (b *BranchSplit) Title() string {
	return b.model.Title()
}

// WithTitle sets the title of the widget.
func (b *BranchSplit) WithTitle(title string) *BranchSplit {
	b.model = b.model.WithTitle(title)
	return b
}

// Description returns the description of the widget.
func (b *BranchSplit) Description() string {
	return b.model.Description()
}

// WithDescription sets the description of the widget.
func (b *BranchSplit) WithDescription(desc string) *BranchSplit {
	b.model = b.model.WithDescription(desc)
	return b
}

// WithHEAD sets the name of the head commit: the last commit in the branch.
// This name, if present, is shown next to the head commit.
func (b *BranchSplit) WithHEAD(head string) *BranchSplit {
	b.head = head
	return b
}

// Err returns nil.
func (b *BranchSplit) Err() error { return nil }

// Init initializes the widget.
func (b *BranchSplit) Init() tea.Cmd {
	options := make([]ui.MultiSelectOption[commit.Summary], len(b.commits))
	for idx, c := range b.commits {
		options[idx] = ui.MultiSelectOption[commit.Summary]{
			Value: c,
			Skip:  idx == len(b.commits)-1,
		}
	}
	b.model = b.model.WithOptions(options...)
	return b.model.Init()
}

// Update updates the widget based on the message.
func (b *BranchSplit) Update(msg tea.Msg) tea.Cmd {
	return b.model.Update(msg)
}

func (b *BranchSplit) renderCommit(w ui.Writer, idx int, option ui.MultiSelectOption[commit.Summary]) {
	commitStyle := b.Style.Commit
	headIdx := len(b.commits) - 1
	switch {
	case idx == headIdx:
		commitStyle = b.Style.HeadCommit
		w.WriteString(b.Style.HeadMarker.String())
	case option.Selected:
		w.WriteString(b.Style.SplitMarker.String())
	default:
		w.WriteString(" ")
	}

	summaryOptions := commit.SummaryOptions{
		Now: _timeNow,
	}

	w.WriteString(" ")
	c := option.Value
	(&commit.Summary{
		ShortHash:  c.ShortHash,
		Subject:    c.Subject,
		AuthorDate: c.AuthorDate,
	}).Render(w, commitStyle, &summaryOptions)

	if idx == headIdx && b.head != "" {
		w.WriteString(" [")
		w.WriteString(b.Style.HeadCommit.Subject.Render(b.head))
		w.WriteString("]")
	}
}

// Render renders the widget to the given writer.
func (b *BranchSplit) Render(w ui.Writer) {
	b.model.Render(w)
}

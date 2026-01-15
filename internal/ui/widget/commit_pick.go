package widget

import (
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/commit"
	"go.abhg.dev/gs/internal/ui/fliptree"
)

// TODO: support multi-select

// CommitPickKeyMap defines the key mappings for the commit pick widget.
type CommitPickKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Accept key.Binding
}

// DefaultCommitPickKeyMap is the default key map for the commit pick widget.
var DefaultCommitPickKeyMap = CommitPickKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("up", "go up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("down", "go down"),
	),
	Accept: key.NewBinding(
		key.WithKeys("enter", "tab"),
		key.WithHelp("enter/tab", "accept"),
	),
}

// CommitPickStyle defines the visual style of the commit pick widget.
type CommitPickStyle struct {
	Branch      lipgloss.Style
	CursorStyle lipgloss.Style

	LogCommitStyle commit.SummaryStyle
}

// DefaultCommitPickStyle is the default style for the commit pick widget.
var DefaultCommitPickStyle = CommitPickStyle{
	Branch: ui.NewStyle().Bold(true),
	CursorStyle: ui.NewStyle().
		Foreground(ui.Yellow).
		Bold(true).
		SetString("▶"),
	LogCommitStyle: commit.DefaultSummaryStyle,
}

// CommitPickBranch is a single branch shown in the commit pick widget.
type CommitPickBranch struct {
	// Branch is the name of the branch.
	Branch string

	// Base is the base branch that this branch is based on.
	// This will be used to create a tree view.
	// If no base is specified, the branch is shown as a root.
	Base string

	// Commits in the branch that we can select from.
	Commits []commit.Summary
}

type commitPickBranch struct {
	Name    string
	Base    int   // index in CommitPick.branches or -1
	Aboves  []int // index in CommitPick.branches
	Commits []int // index in CommitPick.commits
}

type commitPickCommit struct {
	Summary commit.Summary
	Branch  int // index in CommitPick.branches
}

// CommitPick is a widget that allows users to pick out a commit
// from a list of branches and commits.
type CommitPick struct {
	KeyMap CommitPickKeyMap
	Style  CommitPickStyle

	title string
	desc  string

	// Original list of branches provided to WithBranches.
	// Is turned into branches, commits, and commitOrder at Init() time.
	input []CommitPickBranch

	branches []commitPickBranch
	commits  []commitPickCommit
	roots    []int // indexes in branches of root branches (no base)

	// Indexes in commits, ordered by how they're presented.
	// This is depth-first by branch, and then in-order per-branch.
	order  []int
	cursor int // index of cursor in order

	accepted bool
	value    *git.Hash
	err      error
}

var _ ui.Field = (*CommitPick)(nil)

// NewCommitPick initializes a new CommitPick widget.
// Use WithBranches to add branch information.
func NewCommitPick() *CommitPick {
	return &CommitPick{
		KeyMap: DefaultCommitPickKeyMap,
		Style:  DefaultCommitPickStyle,
		value:  new(git.Hash),
	}
}

// Title returns the title of the field.
func (c *CommitPick) Title() string { return c.title }

// Description provides an optional description for the field.
func (c *CommitPick) Description() string { return c.desc }

// Err returns an error if the widget has already failed.
func (c *CommitPick) Err() error { return c.err }

// WithBranches adds branches with commits for a user to select from.
func (c *CommitPick) WithBranches(branches ...CommitPickBranch) *CommitPick {
	c.input = branches
	return c
}

// WithTitle changes the title of the widget.
func (c *CommitPick) WithTitle(title string) *CommitPick {
	c.title = title
	return c
}

// WithDescription changes the description of the widget.
func (c *CommitPick) WithDescription(desc string) *CommitPick {
	c.desc = desc
	return c
}

// WithValue specifies the variable to which the selected commit hash
// will be written.
func (c *CommitPick) WithValue(value *git.Hash) *CommitPick {
	c.value = value
	return c
}

// UnmarshalValue unmarshals a commit hash from an external source.
// This is used by [ui.RobotView] to supply the value in tests.
func (c *CommitPick) UnmarshalValue(unmarshal func(any) error) error {
	var hash git.Hash
	if err := unmarshal(&hash); err != nil {
		return err
	}
	*c.value = hash
	return nil
}

// Init initializes the widget. This is called by Bubble Tea.
// With* functions may not be used once this is called.
func (c *CommitPick) Init() tea.Cmd {
	if len(c.input) == 0 {
		c.err = errors.New("no branches provided")
		return tea.Quit
	}

	// First pass: initialize objects.
	branches := make([]commitPickBranch, 0, len(c.input))
	branchIdxByName := make(map[string]int, len(c.input))
	var commits []commitPickCommit
	for _, b := range c.input {
		idx := len(branches)
		branch := commitPickBranch{
			Name: b.Branch,
			Base: -1,
		}
		branchIdxByName[b.Branch] = idx
		for _, commit := range b.Commits {
			branch.Commits = append(
				branch.Commits, len(commits),
			)
			commits = append(commits, commitPickCommit{
				Summary: commit,
				Branch:  idx,
			})
		}
		branches = append(branches, branch)
	}

	if len(commits) == 0 {
		c.err = errors.New("no commits found")
		return tea.Quit
	}

	// Second pass: connect Bases and Aboves.
	rootSet := make(map[int]struct{})
	for idx, b := range c.input {
		if b.Base == "" {
			rootSet[idx] = struct{}{}
			continue
		}

		baseIdx, ok := branchIdxByName[b.Base]
		if !ok {
			// Base is not a known branch.
			// That's fine, add an empty entry for it.
			baseIdx = len(branches)
			branches = append(branches, commitPickBranch{
				Name: b.Base,
				Base: -1,
			})
			branchIdxByName[b.Base] = baseIdx
			rootSet[baseIdx] = struct{}{}
		}

		branches[idx].Base = baseIdx
		branches[baseIdx].Aboves = append(branches[baseIdx].Aboves, idx)
	}

	// Finally, using this information,
	// traverse the branches in depth-first order
	// to match the order in which the tree will render them.
	// This will be used for the commit ordering.
	roots := slices.Sorted(maps.Keys(rootSet))

	commitOrder := make([]int, 0, len(commits))
	var visitBranch func(int)
	visitBranch = func(idx int) {
		for _, aboveIdx := range branches[idx].Aboves {
			visitBranch(aboveIdx)
		}

		for _, commitIdx := range branches[idx].Commits {
			// If the current (default) value matches the hash,
			// move the cursor to it.
			if commits[commitIdx].Summary.ShortHash == *c.value {
				c.cursor = len(commitOrder)
			}

			commitOrder = append(commitOrder, commitIdx)
		}
	}
	for _, root := range roots {
		visitBranch(root)
	}

	c.branches = branches
	c.commits = commits
	c.order = commitOrder
	c.roots = roots
	return nil
}

// Update receives a UI message and updates the widget's internal state.
func (c *CommitPick) Update(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	// TODO: do we want to support filtering?

	switch {
	case key.Matches(keyMsg, c.KeyMap.Up):
		c.moveCursor(false /* backwards */)
	case key.Matches(keyMsg, c.KeyMap.Down):
		c.moveCursor(true /* forwards */)
	case key.Matches(keyMsg, c.KeyMap.Accept):
		c.accepted = true
		commitIdx := c.order[c.cursor]
		*c.value = c.commits[commitIdx].Summary.ShortHash
		return ui.AcceptField
	}

	return nil
}

func (c *CommitPick) moveCursor(forwards bool) {
	delta := 1
	if !forwards {
		delta = -1
	}

	c.cursor += delta
	if c.cursor < 0 {
		c.cursor = len(c.order) - 1
	} else if c.cursor >= len(c.order) {
		c.cursor = 0
	}
}

// Render renders the widget to a writer.
func (c *CommitPick) Render(w ui.Writer) {
	if c.accepted {
		w.WriteString(c.value.String())
		return
	}

	if c.title != "" {
		w.WriteString("\n")
	}

	summaryOptions := commit.SummaryOptions{
		Now: _timeNow,
	}

	_ = fliptree.Write(w, fliptree.Graph[commitPickBranch]{
		Values: c.branches,
		Roots:  c.roots,
		Edges:  func(b commitPickBranch) []int { return b.Aboves },
		View: func(b commitPickBranch) string {
			var o strings.Builder
			o.WriteString(c.Style.Branch.Render(b.Name))

			focusedCommitIdx := c.order[c.cursor]
			focusedBranchIdx := c.commits[focusedCommitIdx].Branch

			for _, commitIdx := range b.Commits {
				commit := c.commits[commitIdx]

				o.WriteString(" ")
				o.WriteString("\n")

				cursor := " "
				summaryStyle := c.Style.LogCommitStyle
				// Three levels of visibility for commits:
				//  1. focused on commit
				//  2. not focused on commit,
				//     but focused on commit in same branch
				//  3. focused on a different branch
				switch {
				case focusedCommitIdx == commitIdx:
					summaryStyle = summaryStyle.Bold(true)
					cursor = c.Style.CursorStyle.String()
				case focusedBranchIdx == commit.Branch:
					// default style is good enough
				default:
					summaryStyle = summaryStyle.Faint(true)
				}

				o.WriteString(cursor)
				o.WriteString(" ")
				commit.Summary.Render(&o, summaryStyle, &summaryOptions)
			}

			return o.String()
		},
	}, fliptree.Options[commitPickBranch]{})
}

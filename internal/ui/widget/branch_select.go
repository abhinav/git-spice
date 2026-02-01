package widget

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/branchtree"
)

// BranchSelectKeyMap defines the key bindings for [Select].
type BranchSelectKeyMap struct {
	Up   key.Binding // move up the list
	Down key.Binding // move down the list

	Accept  key.Binding // accept the focused option
	Delete  key.Binding // delete the last character in the filter
	Discard key.Binding // discard the current selection
}

// DefaultBranchSelectKeyMap is the default key map for a [Select].
var DefaultBranchSelectKeyMap = BranchSelectKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "ctrl+k"),
		key.WithHelp("up", "go up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "ctrl+j"),
		key.WithHelp("down", "go down"),
	),
	Accept: key.NewBinding(
		key.WithKeys("enter", "tab"),
		key.WithHelp("enter/tab", "accept"),
	),
	Delete: key.NewBinding(
		key.WithKeys("backspace", "ctrl+h"),
		key.WithHelp("backspace", "delete filter character"),
	),
	Discard: key.NewBinding(
		key.WithKeys("ctrl+u"),
		key.WithHelp("ctrl+u", "delete filter line"),
	),
}

// _branchSelectStyle is the default style for a [BranchTreeSelect] widget.
// It modifies branchtree.DefaultStyle to match the widget's visual appearance.
var _branchSelectStyle = func() branchtree.Style {
	s := branchtree.DefaultStyle
	s.Branch = ui.NewStyle()
	s.BranchHighlighted = ui.NewStyle().Bold(true).Foreground(ui.Yellow)
	return s
}()

// BranchTreeItem is a single item in a [BranchTreeSelect].
type BranchTreeItem struct {
	// Branch is the name of the branch.
	Branch string

	// ChangeID is the optional change ID associated with this branch.
	// It will be appended to the branch name.
	ChangeID string

	// Worktree is the absolute path to the worktree where this branch is checked out.
	// Empty if the branch is not checked out.
	Worktree string

	// Base is the name of the branch this branch is on top of.
	// This will be used to create a tree view of branches.
	// Branches with no base are considered root branches.
	Base string

	// Disabled indicates that this branch cannot be selected.
	// It will appear grayed out in the list.
	Disabled bool
}

type branchInfo struct {
	BranchTreeItem

	Index              int   // index in all
	Aboves             []int // indexes of branches in 'all' with this as base
	BranchHighlights   []int // indexes of runes in Branch name to highlight
	ChangeIDHighlights []int // indexes of runes in ChangeID to highlight
	WorktreeHighlights []int // indexes of runes in Worktree to highlight
	Visible            bool  // whether this branch is visible
}

// BranchTreeSelect is a prompt that allows selecting a branch
// from a tree-view of branches.
// The trunk branch is shown at the bottom of the tree similarly to 'gs ls'.
//
// In addition to arrow-based navigation,
// it allows fuzzy filtering branches by typing the branch name.
type BranchTreeSelect struct {
	Style  *branchtree.Style
	KeyMap BranchSelectKeyMap

	all       []*branchInfo  // all known branches
	roots     []int          // indexes in 'all' of root branches
	idxByName map[string]int // index in 'all' by branch name

	selectable []int // indexes that can be selected and are visible
	focused    int   // index in 'selectable' of the currently focused branch

	filter []rune // filter text
	err    error

	title           string
	desc            string
	value           *string // selected branch name
	accepted        bool    // whether the current selection has been accepted
	currentWorktree string  // absolute path to current worktree
}

var _ ui.Field = (*BranchTreeSelect)(nil)

// NewBranchTreeSelect creates a new [BranchTreeSelect] widget.
func NewBranchTreeSelect() *BranchTreeSelect {
	return &BranchTreeSelect{
		KeyMap:    DefaultBranchSelectKeyMap,
		idxByName: make(map[string]int),
		value:     new(string),
	}
}

// Err reports any errors in the current state of the widget.
func (b *BranchTreeSelect) Err() error {
	return b.err
}

// Value returns the selected branch name.
func (b *BranchTreeSelect) Value() string {
	return *b.value
}

// WithValue specifies the destination for the selected branch.
// If the existing value matches a branch name, it will be selected.
func (b *BranchTreeSelect) WithValue(value *string) *BranchTreeSelect {
	b.value = value
	return b
}

// UnmarshalValue unmarshals the value of the field
// using the given unmarshal function.
//
// It accepts one of the following types:
//
//   - bool: if true, the current selection is accepted
//   - string: the name of the selected branch (must be a known branch)
func (b *BranchTreeSelect) UnmarshalValue(unmarshal func(any) error) error {
	if ok := new(bool); unmarshal(ok) == nil && *ok {
		if b.focused >= 0 && b.focused < len(b.selectable) {
			*b.value = b.all[b.selectable[b.focused]].Branch
			b.accepted = true
			return nil
		}

		return errors.New("no branch selected")
	}

	var got string
	if err := unmarshal(&got); err != nil {
		return err
	}

	for _, bi := range b.all {
		if bi.Branch == got && !bi.Disabled {
			*b.value = got
			b.accepted = true
			return nil
		}
	}

	return fmt.Errorf("unknown branch: %s", got)
}

// Init initializes the widget.
func (b *BranchTreeSelect) Init() tea.Cmd {
	rootSet := make(map[int]struct{})

	// Connect the branches to their bases,
	// and track which branches are root branches.
	selected := -1
	for _, bi := range b.all {
		bi.Visible = true
		if bi.Branch == *b.value {
			selected = bi.Index
		}
		if bi.Base == "" {
			rootSet[bi.Index] = struct{}{}
			continue
		}

		var base *branchInfo
		if idx, ok := b.idxByName[bi.Base]; ok {
			base = b.all[idx]
		} else {
			// This branch is not in the list of inputs
			// and so it isn't selectable,
			// but it still needs to be shown
			// for the tree to render correctly.
			base = &branchInfo{
				Index:   len(b.all),
				Visible: true,
				BranchTreeItem: BranchTreeItem{
					Branch:   bi.Base,
					Disabled: true,
				},
			}
			b.all = append(b.all, base)
			b.idxByName[base.Branch] = base.Index
			rootSet[base.Index] = struct{}{}
		}

		base.Aboves = append(base.Aboves, bi.Index)
	}

	roots := make([]int, 0, len(rootSet))
	for idx := range rootSet {
		roots = append(roots, idx)
	}
	sort.Ints(roots)
	b.roots = roots

	b.updateSelectable()
	if selected >= 0 {
		b.focused = max(slices.Index(b.selectable, selected), 0)
	}

	return nil
}

// Title returns the title of the widget.
func (b *BranchTreeSelect) Title() string {
	return b.title
}

// WithTitle sets the title of the widget.
func (b *BranchTreeSelect) WithTitle(title string) *BranchTreeSelect {
	b.title = title
	return b
}

// Description returns the description of the widget.
func (b *BranchTreeSelect) Description() string {
	return b.desc
}

// WithDescription sets the description of the widget.
func (b *BranchTreeSelect) WithDescription(description string) *BranchTreeSelect {
	b.desc = description
	return b
}

// WithCurrentWorktree sets the current worktree path.
// Branches checked out in this worktree will not show a worktree indicator.
func (b *BranchTreeSelect) WithCurrentWorktree(path string) *BranchTreeSelect {
	b.currentWorktree = path
	return b
}

// WithItems adds a branch and its base to the widget with the given base.
// The named branch can be selected, but the base cannot.
func (b *BranchTreeSelect) WithItems(items ...BranchTreeItem) *BranchTreeSelect {
	for _, item := range items {
		idx := len(b.all)
		b.all = append(b.all, &branchInfo{
			BranchTreeItem: item,
			Index:          idx,
		})
		b.idxByName[item.Branch] = idx
	}
	return b
}

// Update updates the state of the widget based on a bubbletea message.
func (b *BranchTreeSelect) Update(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	var filterChanged bool
	switch {
	case key.Matches(keyMsg, b.KeyMap.Up):
		b.moveCursor(-1)

	case key.Matches(keyMsg, b.KeyMap.Down):
		b.moveCursor(1)

	case key.Matches(keyMsg, b.KeyMap.Accept):
		if b.focused >= 0 && b.focused < len(b.selectable) {
			*b.value = b.all[b.selectable[b.focused]].Branch
			b.accepted = true
			return ui.AcceptField
		}

	case key.Matches(keyMsg, b.KeyMap.Delete):
		if len(b.filter) > 0 {
			b.filter = b.filter[:len(b.filter)-1]
			filterChanged = true
		}

	case key.Matches(keyMsg, b.KeyMap.Discard):
		if len(b.filter) > 0 {
			b.filter = b.filter[:0]
			filterChanged = true
		}

	case keyMsg.Type == tea.KeyRunes:
		for _, r := range keyMsg.Runes {
			b.filter = append(b.filter, unicode.ToLower(r))
		}
		filterChanged = true
	}

	if filterChanged {
		b.updateSelectable()
	}

	return nil
}

func (b *BranchTreeSelect) moveCursor(delta int) {
	// Nothing to select.
	if len(b.selectable) == 0 {
		return
	}

	b.focused = (b.focused + delta) % len(b.selectable)
	if b.focused < 0 {
		b.focused += len(b.selectable)
	}
}

func (b *BranchTreeSelect) updateSelectable() {
	b.err = nil

	selected := -1
	if b.focused >= 0 && b.focused < len(b.selectable) {
		selected = b.selectable[b.focused]
	}

	b.selectable = b.selectable[:0]
	var visit func(int)
	visit = func(idx int) {
		for _, above := range b.all[idx].Aboves {
			visit(above)
		}

		visible := b.matchesFilter(b.all[idx])
		b.all[idx].Visible = visible
		if visible && !b.all[idx].Disabled {
			b.selectable = append(b.selectable, idx)
		}
	}

	// Depth-first traversal gives us the same order
	// as the tree view.
	for _, root := range b.roots {
		visit(root)
	}

	if len(b.selectable) == 0 {
		b.err = fmt.Errorf("no available matches: %s", string(b.filter))
		return
	}

	if len(b.filter) == 0 {
		// choose the default selected if no filter
		b.focused = max(slices.Index(b.selectable, selected), 0)
		return
	}
	// rank the selectable branches
	branches := make([]string, len(b.selectable))
	for i, idx := range b.selectable {
		branches[i] = b.all[idx].Branch
	}
	matches := fuzzy.Find(string(b.filter), branches)
	bestSelectable := selected
	if len(matches) > 0 {
		bestSelectable = b.selectable[matches[0].Index]
	}
	b.focused = max(slices.Index(b.selectable, bestSelectable), 0)
}

func (b *BranchTreeSelect) matchesFilter(bi *branchInfo) bool {
	type filterMatcher struct {
		str string // string to match
		hls *[]int // highlights to update
	}

	matchers := []filterMatcher{
		{bi.Branch, &bi.BranchHighlights},
		{bi.ChangeID, &bi.ChangeIDHighlights},
	}

	// Only match worktree if it will be displayed.
	// Worktrees matching the current worktree are not shown.
	if bi.Worktree != "" && bi.Worktree != b.currentWorktree {
		matchers = append(matchers, filterMatcher{bi.Worktree, &bi.WorktreeHighlights})
	}

	// Always reset highlights regardless of filter/matches.
	for _, m := range matchers {
		*m.hls = (*m.hls)[:0]
	}

	if len(b.filter) == 0 {
		return true
	}

	// Consider this matched if _any_ matcher had results.
	var matched bool
	for _, m := range matchers {
		if len(m.str) == 0 {
			continue
		}

		matches := fuzzy.Find(string(b.filter), []string{m.str})
		if len(matches) > 0 {
			*m.hls = matches[0].MatchedIndexes
			matched = true
		}
	}

	return matched
}

// Render renders the widget.
func (b *BranchTreeSelect) Render(w ui.Writer) {
	if b.accepted {
		w.WriteString(b.Value())
		return
	}

	if b.title != "" {
		w.WriteString("\n")
	}

	// visibleDescendants fills dst with the indexes of visible descendants
	// of the branches in start.
	// If a branch is visible, it is added; otherwise, its children are checked.
	var visibleDescendants func([]int, []int) []int
	visibleDescendants = func(starts []int, visibles []int) []int {
		for _, idx := range starts {
			if b.all[idx].Visible {
				visibles = append(visibles, idx)
				continue
			}
			visibles = visibleDescendants(b.all[idx].Aboves, visibles)
		}
		return visibles
	}

	selected := -1
	if b.focused >= 0 && b.focused < len(b.selectable) {
		selected = b.selectable[b.focused]
	}

	// Convert branchTreeSelectItemInfo to BranchTreeItem.
	items := make([]*branchtree.Item, len(b.all))
	for i, bi := range b.all {
		items[i] = &branchtree.Item{
			Branch:             bi.Branch,
			ChangeID:           bi.ChangeID,
			Worktree:           bi.Worktree,
			Aboves:             visibleDescendants(bi.Aboves, nil),
			Highlighted:        bi.Index == selected,
			Disabled:           bi.Disabled,
			BranchHighlights:   bi.BranchHighlights,
			ChangeIDHighlights: bi.ChangeIDHighlights,
			WorktreeHighlights: bi.WorktreeHighlights,
		}
	}

	g := branchtree.Graph{
		Items: items,
		Roots: visibleDescendants(b.roots, nil),
	}

	var home string
	if h, err := os.UserHomeDir(); err == nil {
		home = h
	}

	_ = branchtree.Write(w, g, &branchtree.GraphOptions{
		Style:           cmp.Or(b.Style, &_branchSelectStyle),
		CurrentWorktree: b.currentWorktree,
		HomeDir:         home,
	})
}

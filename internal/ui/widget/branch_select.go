package widget

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
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

// DefaultBranchSelectStyle is the default style for a [BranchTreeSelect] widget.
// It modifies branchtree.DefaultStyle to match the widget's visual appearance.
var DefaultBranchSelectStyle = func() branchtree.Style {
	s := branchtree.DefaultStyle
	s.Branch = ui.NewStyle()
	s.BranchHighlighted = ui.NewStyle().
		Bold(true).
		Foreground(ui.Yellow)
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
	VisibleAboves      []int // visible children to render above this branch
	BranchHighlights   []int // indexes of runes in Branch name to highlight
	ChangeIDHighlights []int // indexes of runes in ChangeID to highlight
	WorktreeHighlights []int // indexes of runes in Worktree to highlight
	Visible            bool  // whether this branch is visible
}

// BranchTreeSelect is a prompt that allows selecting a branch
// from a tree-view of branches.
// The trunk branch is shown at the bottom of the tree similarly to 'git-spice ls'.
//
// In addition to arrow-based navigation,
// it allows fuzzy filtering branches by typing the branch name.
type BranchTreeSelect struct {
	Style  *branchtree.Style
	KeyMap BranchSelectKeyMap

	all       []*branchInfo  // all known branches
	roots     []int          // indexes in 'all' of root branches
	idxByName map[string]int // index in 'all' by branch name

	selectable   []int // indexes that can be selected and are visible
	visibleRoots []int // visible root indexes after filtering hidden intermediates
	visibleOrder []int // visible branch indexes in render order
	focused      int   // index in 'selectable' of the currently focused branch
	visible      int   // number of visible rows in the branch list
	offset       int   // offset of the first visible row

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
	b.syncViewport()

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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.visible = max(1, msg.Height-5)
		b.syncViewport()
		return nil

	case tea.KeyMsg:
		var filterChanged bool
		switch {
		case key.Matches(msg, b.KeyMap.Up):
			b.moveCursor(-1)

		case key.Matches(msg, b.KeyMap.Down):
			b.moveCursor(1)

		case key.Matches(msg, b.KeyMap.Accept):
			if b.focused >= 0 && b.focused < len(b.selectable) {
				*b.value = b.all[b.selectable[b.focused]].Branch
				b.accepted = true
				return ui.AcceptField
			}

		case key.Matches(msg, b.KeyMap.Delete):
			if len(b.filter) > 0 {
				b.filter = b.filter[:len(b.filter)-1]
				filterChanged = true
			}

		case key.Matches(msg, b.KeyMap.Discard):
			if len(b.filter) > 0 {
				b.filter = b.filter[:0]
				filterChanged = true
			}

		case msg.Key().Text != "":
			for _, r := range msg.Key().Text {
				b.filter = append(b.filter, unicode.ToLower(r))
			}
			filterChanged = true
		}

		if filterChanged {
			b.updateSelectable()
		}

		b.syncViewport()
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

	// Rebuild all filtered tree state from the full branch graph.
	//
	// Given roots main -> feat1 -> feat1.1,
	// if only feat1.1 matches, the derived visible tree is:
	//   visibleRoots = [feat1.1]
	//   visibleOrder = [feat1.1]
	//   selectable = [feat1.1] if it is not disabled
	//
	// At the end of this method:
	//   - bi.Visible reports whether the branch itself matched
	//   - bi.VisibleAboves lists visible children rendered beneath that branch
	//   - visibleRoots is the filtered forest passed to Render
	//   - visibleOrder is the tree order used for viewport sync
	//   - selectable is the focusable subset of visibleOrder
	b.selectable = b.selectable[:0]
	b.visibleRoots = b.visibleRoots[:0]
	b.visibleOrder = b.visibleOrder[:0]

	// visit takes the index of a branch in the full tree.
	// It returns that branch if it is visible,
	// otherwise it returns the branch's visible descendants.
	//
	// In either case, the branch's state (Visible, VisibleAboves, etc.)
	// is updated based on the filter.
	var visit func(int) []int
	visit = func(idx int) []int {
		bi := b.all[idx]

		var visibleChildren []int
		for _, above := range bi.Aboves {
			visibleChildren = append(visibleChildren, visit(above)...)
		}

		bi.Visible = b.matchesFilter(bi)
		if bi.Visible {
			// Visible branches stay in the rendered tree
			// and keep the visible descendants found under them.
			bi.VisibleAboves = append(bi.VisibleAboves[:0], visibleChildren...)
			b.visibleOrder = append(b.visibleOrder, idx)
			if !bi.Disabled {
				b.selectable = append(b.selectable, idx)
			}
			return []int{idx}
		}

		// Hidden branches are removed from the rendered tree,
		// but their visible descendants are promoted upward
		// to the nearest visible ancestor or the root list.
		bi.VisibleAboves = bi.VisibleAboves[:0]
		return visibleChildren
	}

	// Depth-first traversal gives us the same order
	// as the tree view.
	for _, root := range b.roots {
		b.visibleRoots = append(b.visibleRoots, visit(root)...)
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

	if selectedIdx := slices.Index(b.selectable, selected); selectedIdx >= 0 {
		b.focused = selectedIdx
		return
	}

	// rank the selectable branches
	branches := make([]string, len(b.selectable))
	for i, idx := range b.selectable {
		branches[i] = b.all[idx].Branch
	}
	matches := fuzzy.Find(string(b.filter), branches)
	bestSelectable := b.selectable[0]
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
func (b *BranchTreeSelect) Render(w ui.Writer, theme ui.Theme) {
	if b.accepted {
		w.WriteString(b.Value())
		return
	}

	if b.title != "" {
		w.WriteString("\n")
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
			Aboves:             bi.VisibleAboves,
			Highlighted:        bi.Index == selected,
			Disabled:           bi.Disabled,
			BranchHighlights:   bi.BranchHighlights,
			ChangeIDHighlights: bi.ChangeIDHighlights,
			WorktreeHighlights: bi.WorktreeHighlights,
		}
	}

	g := branchtree.Graph{
		Items: items,
		Roots: b.visibleRoots,
	}

	var home string
	if h, err := os.UserHomeDir(); err == nil {
		home = h
	}

	_ = branchtree.Write(w, g, &branchtree.GraphOptions{
		Theme:           theme,
		Style:           cmp.Or(b.Style, &DefaultBranchSelectStyle),
		CurrentWorktree: b.currentWorktree,
		HomeDir:         home,
		Offset:          b.offset,
		Height:          b.visible,
	})
}

func (b *BranchTreeSelect) syncViewport() {
	if b.visible <= 0 {
		b.offset = 0
		return
	}

	if len(b.visibleOrder) == 0 {
		b.offset = 0
		return
	}

	// Clamp the offset first so window resizes
	// cannot leave the viewport beyond the rendered tree.
	maxOffset := max(0, len(b.visibleOrder)-b.visible)
	b.offset = min(b.offset, maxOffset)

	if b.focused < 0 || b.focused >= len(b.selectable) {
		return
	}

	// The cursor tracks an index into selectable branches,
	// but the viewport scrolls over the full rendered tree.
	selected := b.selectable[b.focused]
	focused := slices.Index(b.visibleOrder, selected)
	if focused < 0 {
		// The focused branch should always be part of the visible tree.
		// If it is not, leave the viewport unchanged rather than
		// applying a bogus offset.
		return
	}

	// Keep the focused branch inside the viewport
	// while preserving the existing scroll position when possible.
	switch {
	case focused < b.offset:
		b.offset = focused // scroll up
	case focused >= b.offset+b.visible:
		b.offset = focused - b.visible + 1 // scroll down
	}
}

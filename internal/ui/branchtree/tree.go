package branchtree

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/commit"
	"go.abhg.dev/gs/internal/ui/fliptree"
)

// Graph holds the tree structure for rendering.
// Pass this into [Write] to render a branch tree.
type Graph struct {
	// Items is the list of all branch items.
	Items []*Item

	// Roots lists indexes of root branches (those with no base)
	// in the Items list.
	Roots []int
}

// Item represents a single branch in a rendered tree.
type Item struct {
	// Branch is the name of the branch.
	Branch string

	// Aboves lists indexes of branches stacked directly above this one.
	// These indexes refer to positions in Graph.Items.
	Aboves []int

	// BranchHighlights contains rune indexes in [Branch] to highlight.
	// Characters at these indexes use Style.TextHighlight.
	BranchHighlights []int

	// TODO: Combine (string, []int) pairs into a HighlightedString type?

	// ChangeID is the optional change ID or URL to display.
	//
	// If non-empty, rendered as "($id)" or "($id $state)"
	// depending on ChangeState.
	ChangeID string

	// ChangeURL is the URL for the change request.
	// If non-empty, ChangeID text is rendered as an OSC 8 hyperlink.
	ChangeURL string

	// ChangeIDHighlights contains rune indexes in [ChangeID] to highlight.
	// Characters at these indexes use Style.TextHighlight.
	ChangeIDHighlights []int

	// ChangeState reports whether the change is open, closed, or merged.
	// Only rendered if ChangeID is also set.
	// nil indicates state is not available.
	ChangeState *forge.ChangeState

	// CommentCounts holds comment resolution counts for the change.
	// If non-nil and Total > 0, rendered as " [☑️Resolved/Total💬]".
	CommentCounts *forge.CommentCounts

	// Worktree is the absolute path where this branch is checked out.
	// If non-empty and differs from GraphOptions.CurrentWorktree,
	// rendered as "[wt: path]".
	Worktree string

	// WorktreeHighlights contains rune indexes in [Worktree] to highlight.
	// Characters at these indexes use Style.TextHighlight.
	WorktreeHighlights []int

	// Commits is an optional list of commits to render below the branch.
	// Each commit renders on its own line.
	//
	// Style depends on Highlighted: normal if true, faint otherwise.
	Commits []commit.Summary

	// Submodules maps submodule paths
	// to associated branch names.
	// Rendered as "[path:branch, ...]" when non-empty.
	Submodules map[string]string

	// NeedsRestack indicates whether the branch needs restacking.
	// If true, renders the needs-restack indicator.
	NeedsRestack bool

	// PushStatus contains push-related information.
	// Rendered according to GraphOptions.PushStatusFormat.
	PushStatus PushStatus

	// Highlighted indicates this is the current/selected branch.
	//
	// When true:
	//   - Node marker uses filled square (Style.NodeMarkerHighlighted)
	//   - Trailing marker is shown (Style.Marker)
	//   - Commits use normal style instead of faint
	//
	// It is invalid for both Disabled and Highlighted to be true.
	Highlighted bool

	// Disabled renders this branch faintly
	// to indicate that this entry is disabled.
	//
	// When true:
	//   - Node marker uses faint style (Style.NodeMarkerDisabled)
	//   - Branch name uses faint style
	//
	// It is invalid for both Disabled and Highlighted to be true.
	Disabled bool

	// IntegrationTip indicates this branch is a configured tip of the
	// integration branch. Rendered as a trailing "[integration-tip]"
	// annotation.
	IntegrationTip bool

	// Integration is non-nil for the synthetic integration branch row.
	// The row gets a distinct node marker and a trailing
	// "[integration: N tips]" annotation.
	Integration *Integration

	// TODO: enum for highlighted/disabled state?
}

// Integration holds presentation-relevant data for the integration
// branch row.
type Integration struct {
	// TipCount is the number of configured tips.
	TipCount int
}

// PushStatus contains push-related information
// if the branch has been pushed to a remote.
type PushStatus struct {
	// Ahead is the number of commits ahead of the remote.
	Ahead int

	// Behind is the number of commits behind the remote.
	Behind int

	// NeedsPush indicates whether the branch has unpushed commits.
	//
	// This is true if either Ahead or Behind is non-zero.
	NeedsPush bool
}

// Style defines visual styling for branch items.
type Style struct {
	// Branch styles the branch name for normal items.
	Branch ui.Style

	// BranchHighlighted styles the branch name for highlighted items.
	BranchHighlighted ui.Style

	// BranchDisabled styles the branch name for disabled items.
	BranchDisabled ui.Style

	// ChangeID styles the change ID/URL text.
	ChangeID ui.Style

	// ChangeState styles for different change states.
	// Each style must include the text via SetString.
	ChangeState ChangeStateStyle

	// CommentCounts styles the comment counts indicator.
	CommentCounts ui.Style

	// CommentCountsResolved styles comment counts
	// when all comments are resolved.
	CommentCountsResolved ui.Style

	// Worktree styles the worktree indicator.
	Worktree ui.Style

	// PushStatus styles the push status text.
	PushStatus ui.Style

	// NeedsRestack styles the needs-restack indicator.
	// Must include the text "(needs restack)" via SetString.
	NeedsRestack ui.Style

	// NodeMarker is the default node marker style.
	// Must include the marker character via SetString.
	NodeMarker ui.Style

	// NodeMarkerHighlighted styles the node marker for highlighted items.
	// Must include the marker character via SetString.
	NodeMarkerHighlighted ui.Style

	// NodeMarkerDisabled styles the node marker for disabled items.
	// Must include the marker character via SetString.
	NodeMarkerDisabled ui.Style

	// NodeMarkerIntegration styles the node marker for the integration
	// branch row. Must include the marker character via SetString.
	NodeMarkerIntegration ui.Style

	// Integration styles the trailing "[integration: N tips]"
	// annotation on the integration branch row.
	Integration ui.Style

	// IntegrationTip styles the trailing "[integration-tip]"
	// annotation on configured tip branches.
	IntegrationTip ui.Style

	// TextHighlight styles characters matching fuzzy search.
	TextHighlight ui.Style

	// Marker is the trailing selection marker shown for highlighted items.
	// Must include the marker character via SetString.
	Marker ui.Style
}

// ChangeStateStyle styles different change states.
type ChangeStateStyle struct {
	// Open styles the "open" state text.
	// Must include text via SetString.
	Open ui.Style

	// Closed styles the "closed" state text.
	// Must include text via SetString.
	Closed ui.Style

	// Merged styles the "merged" state text.
	// Must include text via SetString.
	Merged ui.Style
}

// DefaultStyle is the default style for rendering branch trees.
// Copy and modify this to create custom styles.
var DefaultStyle = Style{
	Branch:            ui.NewStyle().Bold(true),
	BranchHighlighted: ui.NewStyle().Bold(true).Foreground(ui.Cyan),
	BranchDisabled:    ui.NewStyle().Foreground(ui.Gray),
	ChangeID:          ui.NewStyle(),
	ChangeState: ChangeStateStyle{
		Open:   ui.NewStyle().Foreground(ui.Green).SetString("open"),
		Closed: ui.NewStyle().Foreground(ui.Gray).SetString("closed"),
		Merged: ui.NewStyle().Foreground(ui.Magenta).SetString("merged"),
	},
	CommentCounts:         ui.NewStyle().Foreground(ui.Yellow),
	CommentCountsResolved: ui.NewStyle().Foreground(ui.Green),
	Worktree:              ui.NewStyle().Faint(true),
	PushStatus:            ui.NewStyle().Foreground(ui.Yellow).Faint(true),
	NeedsRestack:          ui.NewStyle().Foreground(ui.Gray).SetString("(needs restack)"),
	NodeMarker:            fliptree.DefaultNodeMarker,
	NodeMarkerHighlighted: fliptree.DefaultNodeMarker.SetString("■"),
	NodeMarkerDisabled:    fliptree.DefaultNodeMarker.Faint(true),
	NodeMarkerIntegration: fliptree.DefaultNodeMarker.SetString("◆").Foreground(ui.Magenta),
	Integration:           ui.NewStyle().Foreground(ui.Magenta),
	IntegrationTip:        ui.NewStyle().Foreground(ui.Magenta).Faint(true),
	TextHighlight:         ui.NewStyle().Foreground(ui.Cyan),
	Marker:                ui.NewStyle().Foreground(ui.Yellow).Bold(true).SetString("◀"),
}

// GraphOptions configures branch tree rendering.
type GraphOptions struct {
	// Theme defines the theme for default styles.
	Theme ui.Theme

	// Style defines visual styling for all items.
	// If nil, DefaultStyle is used.
	Style *Style

	// CommitStyle defines styling for commit summaries.
	// If nil, commit.DefaultSummaryStyle is used.
	CommitStyle *commit.SummaryStyle

	// PushStatusFormat controls how push status is rendered.
	// Default is PushStatusDisabled (nothing rendered).
	PushStatusFormat PushStatusFormat

	// CurrentWorktree is the path to the current worktree.
	// Branches checked out in this worktree won't show "[wt: ...]".
	CurrentWorktree string

	// HomeDir is used for "~" substitution in worktree paths.
	// If empty, no substitution is performed.
	HomeDir string

	// Offset is the number of rendered lines to skip.
	Offset int

	// Height is the maximum number of rendered lines to show.
	// Zero or negative means render all lines.
	Height int
}

// PushStatusFormat controls how push status is rendered.
type PushStatusFormat int

const (
	// PushStatusDisabled renders nothing for push status.
	PushStatusDisabled PushStatusFormat = iota

	// PushStatusSimple renders "(needs push)" if NeedsPush is true,
	// otherwise renders nothing.
	PushStatusSimple

	// PushStatusAheadBehind renders "(⇡N⇣M)" showing ahead/behind counts.
	// Only rendered if either Ahead or Behind is non-zero.
	PushStatusAheadBehind
)

// Write renders the branch tree to w.
func Write(w io.Writer, g Graph, opts *GraphOptions) error {
	if opts == nil {
		opts = &GraphOptions{}
	}

	if opts.Style == nil {
		opts.Style = &DefaultStyle
	}
	style := opts.Style.resolve(opts.Theme)
	renderer := branchTreeRenderer{
		Theme:            opts.Theme,
		Style:            style,
		CommitStyle:      *cmp.Or(opts.CommitStyle, &commit.DefaultSummaryStyle),
		PushStatusFormat: opts.PushStatusFormat,
		CurrentWorktree:  opts.CurrentWorktree,
		HomeDir:          opts.HomeDir,
	}

	treeStyle := fliptree.DefaultStyle[*Item]()
	treeStyle.NodeMarker = func(item *Item) ui.Style {
		switch {
		case item.Integration != nil:
			return opts.Style.NodeMarkerIntegration
		case item.Disabled:
			return opts.Style.NodeMarkerDisabled
		case item.Highlighted:
			return opts.Style.NodeMarkerHighlighted
		default:
			return opts.Style.NodeMarker
		}
	}

	return fliptree.Write(w, fliptree.Graph[*Item]{
		Values: g.Items,
		Roots:  g.Roots,
		Edges:  func(item *Item) []int { return item.Aboves },
		View:   renderer.RenderItem,
	}, fliptree.Options[*Item]{
		Theme:  opts.Theme,
		Style:  treeStyle,
		Offset: opts.Offset,
		Height: opts.Height,
	})
}

type branchTreeRenderer struct {
	Theme            ui.Theme
	Style            branchTreeStyle
	CommitStyle      commit.SummaryStyle
	PushStatusFormat PushStatusFormat
	CurrentWorktree  string
	HomeDir          string
}

func (r *branchTreeRenderer) RenderItem(item *Item) string {
	var sb strings.Builder
	r.item(&sb, item)
	return sb.String()
}

func (r *branchTreeRenderer) item(sb *strings.Builder, item *Item) {
	// The integration row renders as its own root in the fliptree,
	// which means the tree skips the node marker for it. Re-render
	// the marker inline so the row still has a visual anchor.
	if item.Integration != nil {
		sb.WriteString(r.Style.NodeMarkerIntegration.String())
		sb.WriteString(" ")
	}

	r.branchName(sb, item)

	if item.ChangeID != "" {
		r.changeID(sb, item.ChangeID, item.ChangeURL, item.ChangeIDHighlights, item.ChangeState)
	}

	if cc := item.CommentCounts; cc != nil && cc.Total > 0 {
		r.commentCounts(sb, cc)
	}

	if wt := item.Worktree; wt != "" && wt != r.CurrentWorktree {
		r.worktree(sb, item.Worktree, item.WorktreeHighlights)
	}

	if len(item.Submodules) > 0 {
		r.submodules(sb, item.Submodules)
	}

	if item.NeedsRestack {
		sb.WriteString(" ")
		sb.WriteString(r.Style.NeedsRestack.String())
	}

	if item.Integration != nil {
		noun := "tips"
		if item.Integration.TipCount == 1 {
			noun = "tip"
		}
		sb.WriteString(r.Style.Integration.Render(
			fmt.Sprintf(" [integration: %d %s]", item.Integration.TipCount, noun),
		))
	}

	if item.IntegrationTip {
		sb.WriteString(r.Style.IntegrationTip.Render(" [integration-tip]"))
	}

	r.pushStatus(sb, item.PushStatus)

	if item.Highlighted {
		sb.WriteString(" ")
		sb.WriteString(r.Style.Marker.String())
	}

	if len(item.Commits) > 0 {
		r.commits(sb, item.Highlighted, item.Commits)
	}
}

// branchName renders the branch name with fuzzy highlighting.
func (r *branchTreeRenderer) branchName(sb *strings.Builder, item *Item) {
	baseStyle := r.Style.Branch
	switch {
	case item.Highlighted:
		baseStyle = r.Style.BranchHighlighted
	case item.Disabled:
		baseStyle = r.Style.BranchDisabled
	}

	renderTextWithHighlights(sb, item.Branch, item.BranchHighlights, baseStyle, r.Style.TextHighlight)
}

func (r *branchTreeRenderer) changeID(
	sb *strings.Builder,
	changeID string,
	changeURL string,
	changeIDHighlights []int,
	changeState *forge.ChangeState,
) {
	sb.WriteString(" (")
	defer sb.WriteString(")")

	var inner strings.Builder
	renderTextWithHighlights(&inner, changeID, changeIDHighlights, r.Style.ChangeID, r.Style.TextHighlight)
	text := inner.String()
	if changeURL != "" {
		text = lipgloss.NewStyle().Hyperlink(changeURL).Render(text)
	}
	sb.WriteString(text)

	if changeState != nil {
		sb.WriteString(" ")
		switch *changeState {
		case forge.ChangeOpen:
			sb.WriteString(r.Style.ChangeState.Open.String())
		case forge.ChangeClosed:
			sb.WriteString(r.Style.ChangeState.Closed.String())
		case forge.ChangeMerged:
			sb.WriteString(r.Style.ChangeState.Merged.String())
		}
	}
}

func (r *branchTreeRenderer) commentCounts(
	sb *strings.Builder,
	cc *forge.CommentCounts,
) {
	style := r.Style.CommentCounts
	if cc.Resolved == cc.Total {
		style = r.Style.CommentCountsResolved
	}
	sb.WriteString(style.Render(
		fmt.Sprintf(" [☑️%d/%d💬]", cc.Resolved, cc.Total),
	))
}

func (r *branchTreeRenderer) worktree(
	sb *strings.Builder,
	wt string,
	highlights []int,
) {
	sb.WriteString(r.Style.Worktree.Render(" [wt: "))
	defer sb.WriteString(r.Style.Worktree.Render("]"))

	if r.HomeDir != "" {
		rel, err := filepath.Rel(r.HomeDir, wt)
		if err == nil && filepath.IsLocal(rel) {
			newWT := filepath.Join("~", rel)
			// Replacing "$HOME" prefix in wt with "~"
			// requires shifting the highlights.
			//
			// Any highlights inside range [0:len($HOME))
			// will become '0' to refer to the '~' character.
			//
			// Highlights following that will be shifted left
			// to match the new string by:
			//
			//     len(wt) - len(newWT)
			//
			// Example:
			//
			//              1
			//    01234567890123     01234
			//    /home/user/foo  => ~/foo
			//
			// Indexes are offset by:
			//
			//    len("/home/user/foo") - len("~/foo")
			//    = 14 - 5
			//    = 9
			//
			homeIdx := len(wt) - len(rel)
			offset := len(wt) - len(newWT)

			var adjustedHighlights []int
			for _, idx := range highlights {
				if idx < homeIdx {
					// Highlight the "~" character.
					// If adjustedHighlights is non-empty
					// then we've already added it.
					if len(adjustedHighlights) == 0 {
						adjustedHighlights = append(adjustedHighlights, 0)
					}
					continue
				}

				adjusted := idx - offset
				adjustedHighlights = append(adjustedHighlights, adjusted)
			}
			highlights = adjustedHighlights
			wt = newWT
		}
	}

	renderTextWithHighlights(sb, wt, highlights, r.Style.Worktree, r.Style.TextHighlight)
}

func (r *branchTreeRenderer) submodules(
	sb *strings.Builder,
	subs map[string]string,
) {
	sb.WriteString(r.Style.Worktree.Render(" ["))
	defer sb.WriteString(r.Style.Worktree.Render("]"))

	paths := slices.Sorted(maps.Keys(subs))
	for i, path := range paths {
		if i > 0 {
			sb.WriteString(r.Style.Worktree.Render(", "))
		}
		sb.WriteString(r.Style.Worktree.Render(
			path + ":" + subs[path],
		))
	}
}

func (r *branchTreeRenderer) pushStatus(
	sb *strings.Builder,
	status PushStatus,
) {
	switch r.PushStatusFormat {
	case PushStatusDisabled:
		// Nothing to render.

	case PushStatusSimple:
		if status.NeedsPush {
			sb.WriteString(r.Style.PushStatus.Render(" (needs push)"))
		}

	case PushStatusAheadBehind:
		if status.Ahead > 0 || status.Behind > 0 {
			var parts []string
			if status.Ahead > 0 {
				parts = append(parts, fmt.Sprintf("⇡%d", status.Ahead))
			}
			if status.Behind > 0 {
				parts = append(parts, fmt.Sprintf("⇣%d", status.Behind))
			}
			sb.WriteString(r.Style.PushStatus.Render(" (" + strings.Join(parts, "") + ")"))
		}
	}
}

func (r *branchTreeRenderer) commits(
	sb *strings.Builder,
	highlighted bool,
	commits []commit.Summary,
) {
	commitStyle := r.CommitStyle
	if !highlighted {
		commitStyle = commitStyle.Faint(true)
	}

	for _, commit := range commits {
		sb.WriteString("\n")
		commit.Render(sb, r.Theme, commitStyle, nil /* options */)
	}
}

type branchTreeStyle struct {
	Branch                lipgloss.Style
	BranchHighlighted     lipgloss.Style
	BranchDisabled        lipgloss.Style
	ChangeID              lipgloss.Style
	ChangeState           changeStateStyle
	CommentCounts         lipgloss.Style
	CommentCountsResolved lipgloss.Style
	Worktree              lipgloss.Style
	PushStatus            lipgloss.Style
	NeedsRestack          lipgloss.Style
	NodeMarker            lipgloss.Style
	NodeMarkerHighlighted lipgloss.Style
	NodeMarkerDisabled    lipgloss.Style
	NodeMarkerIntegration lipgloss.Style
	Integration           lipgloss.Style
	IntegrationTip        lipgloss.Style
	TextHighlight         lipgloss.Style
	Marker                lipgloss.Style
}

func (s Style) resolve(theme ui.Theme) branchTreeStyle {
	return branchTreeStyle{
		Branch:                s.Branch.Resolve(theme),
		BranchHighlighted:     s.BranchHighlighted.Resolve(theme),
		BranchDisabled:        s.BranchDisabled.Resolve(theme),
		ChangeID:              s.ChangeID.Resolve(theme),
		ChangeState:           s.ChangeState.resolve(theme),
		CommentCounts:         s.CommentCounts.Resolve(theme),
		CommentCountsResolved: s.CommentCountsResolved.Resolve(theme),
		Worktree:              s.Worktree.Resolve(theme),
		PushStatus:            s.PushStatus.Resolve(theme),
		NeedsRestack:          s.NeedsRestack.Resolve(theme),
		NodeMarker:            s.NodeMarker.Resolve(theme),
		NodeMarkerHighlighted: s.NodeMarkerHighlighted.Resolve(theme),
		NodeMarkerDisabled:    s.NodeMarkerDisabled.Resolve(theme),
		NodeMarkerIntegration: s.NodeMarkerIntegration.Resolve(theme),
		Integration:           s.Integration.Resolve(theme),
		IntegrationTip:        s.IntegrationTip.Resolve(theme),
		TextHighlight:         s.TextHighlight.Resolve(theme),
		Marker:                s.Marker.Resolve(theme),
	}
}

type changeStateStyle struct {
	Open   lipgloss.Style
	Closed lipgloss.Style
	Merged lipgloss.Style
}

func (s ChangeStateStyle) resolve(theme ui.Theme) changeStateStyle {
	return changeStateStyle{
		Open:   s.Open.Resolve(theme),
		Closed: s.Closed.Resolve(theme),
		Merged: s.Merged.Resolve(theme),
	}
}

// renderTextWithHighlights renders text with fuzzy search highlighting.
// Characters at indexes specified in highlights are rendered with highlightStyle.
// All other characters are rendered with baseStyle.
var renderTextWithHighlights = _renderTextWithHighlights

func _renderTextWithHighlights(
	sb *strings.Builder,
	text string,
	highlights []int,
	baseStyle, highlightStyle lipgloss.Style,
) {
	if len(highlights) == 0 {
		sb.WriteString(baseStyle.Render(text))
		return
	}

	var lastRuneIdx int
	runes := []rune(text)
	for _, runeIdx := range highlights {
		sb.WriteString(baseStyle.Render(string(runes[lastRuneIdx:runeIdx])))
		sb.WriteString(highlightStyle.Render(string(runes[runeIdx])))
		lastRuneIdx = runeIdx + 1
	}
	sb.WriteString(baseStyle.Render(string(runes[lastRuneIdx:])))
}

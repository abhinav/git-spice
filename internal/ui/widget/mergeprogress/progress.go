// Package mergeprogress renders progress for a stack merge operation.
package mergeprogress

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"go.abhg.dev/gs/internal/ui"
)

const (
	_defaultWidth = 80
)

// ErrCanceled reports that the user canceled progress rendering.
var ErrCanceled = errors.New("merge progress canceled")

// State is one item's position in the merge queue.
//
// The state belongs to the progress layer,
// so callers can translate forge-specific states before updating the widget.
type State uint8

const (
	// StatePending marks an item that has not started.
	StatePending State = iota

	// StateActive marks an item that is currently being prepared,
	// checked, merged, or otherwise waited on.
	StateActive

	// StateMerged marks an item that has merged successfully.
	StateMerged

	// StateFailed marks an item that stopped the merge operation.
	StateFailed

	// StateSkipped marks an item that does not need to be merged.
	StateSkipped
)

// Item is one slot in the progress bar.
type Item struct {
	// ID uniquely identifies the item for progress updates.
	ID string

	// State controls the glyph and color used for this item.
	State State
}

// Event is a Bubble Tea message that updates [Widget] progress.
//
// Send Event through tea.Program.Send from the operation running alongside
// the Bubble Tea loop.
// The widget applies the event during [Widget.Update].
type Event struct {
	// ItemID identifies the item to update.
	//
	// Empty means the event updates only the operation message.
	ItemID string

	// State is the new state for the item.
	State State

	// Message is the current operation detail shown below the bar.
	Message string
}

// KeyMap defines key bindings for [Widget].
type KeyMap struct {
	Cancel key.Binding
}

// DefaultKeyMap is the default key map for [Widget].
var DefaultKeyMap = KeyMap{
	Cancel: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "cancel"),
	),
}

// GlyphSet defines the characters used for queue states.
type GlyphSet struct {
	Merged  string // merged item
	Active  string // active item base fill
	Pending string // item not yet started
	Failed  string // item that failed
	Skipped string // item skipped by scheduler policy

	Animation AnimationGlyphSet // active item marker glyphs
}

// AnimationGlyphSet defines active-segment animation characters.
type AnimationGlyphSet struct {
	LeftEdge  string // marker at the left edge
	RightEdge string // marker at the right edge
	Slow      string // marker while moving slowly
	Medium    string // marker while moving at medium speed
	Fast      string // marker while moving fastest
}

// SymbolsGlyphSet returns the default symbol glyph set.
func SymbolsGlyphSet() GlyphSet {
	return GlyphSet{
		Merged:  "■",
		Active:  "◆",
		Pending: "□",
		Failed:  "×",
		Skipped: "○",
		Animation: AnimationGlyphSet{
			LeftEdge:  "◆",
			RightEdge: "◆",
			Slow:      "◆",
			Medium:    "◇",
			Fast:      "◇",
		},
	}
}

// ASCIIGlyphSet returns the glyph set for basic terminal compatibility.
func ASCIIGlyphSet() GlyphSet {
	return GlyphSet{
		Merged:  "#",
		Active:  "=",
		Pending: "-",
		Failed:  "x",
		Skipped: "o",
		Animation: AnimationGlyphSet{
			LeftEdge:  "-",
			RightEdge: "-",
			Slow:      "-",
			Medium:    "-",
			Fast:      "-",
		},
	}
}

// Style defines the visual style of [Widget].
type Style struct {
	Merged  ui.Style // requires value
	Active  ui.Style // requires value
	Pending ui.Style // requires value
	Failed  ui.Style // requires value
	Skipped ui.Style // requires value

	Title   ui.Style
	Summary ui.Style
	Detail  ui.Style
	Hint    ui.Style
}

// DefaultStyle is the default visual style of [Widget].
//
// TODO: Add merge configuration plumbing to select [ASCIIGlyphSet]
// for terminals where the symbol glyphs do not render well.
var DefaultStyle = styleForGlyphSet(SymbolsGlyphSet())

// Widget is a Bubble Tea model for stack merge progress.
//
// Callers provide the merge queue with [New],
// then post [Event] messages to update each queue item.
// The widget renders every item into one continuous horizontal bar.
// Terminal width changes each item's segment width,
// but not which fields are rendered:
// branch names stay out of the bar,
// and the current operation detail is shown below it.
//
// The widget does not import or depend on forge APIs.
// Translate forge-specific states into [State] values at the command boundary.
type Widget struct {
	KeyMap KeyMap
	Style  Style

	items     []Item
	index     map[string]int // item ID => items index
	message   string
	err       error
	width     int
	theme     ui.Theme
	animation animationState
	glyphs    GlyphSet
}

var _ tea.Model = (*Widget)(nil)

// New builds a progress widget for the given queue items.
func New(items ...Item) *Widget {
	w := &Widget{
		KeyMap: DefaultKeyMap,
		Style:  DefaultStyle,
		glyphs: SymbolsGlyphSet(),
		animation: animationState{
			enabled: true,
		},
	}
	w.setItems(items...)
	return w
}

func (w *Widget) setItems(items ...Item) {
	w.items = append(w.items[:0], items...)
	w.index = make(map[string]int, len(items))
	for idx, item := range items {
		w.index[item.ID] = idx
	}
}

func (w *Widget) apply(event Event) {
	if event.ItemID != "" {
		if idx, ok := w.index[event.ItemID]; ok {
			w.items[idx].State = event.State
		}
	}
	if event.Message != "" {
		w.message = event.Message
	}
}

// WithTheme sets the theme used by Bubble Tea rendering.
func (w *Widget) WithTheme(theme ui.Theme) *Widget {
	w.theme = theme
	return w
}

// WithGlyphSet changes the queue state characters used by the widget.
func (w *Widget) WithGlyphSet(glyphs GlyphSet) *Widget {
	w.glyphs = glyphs
	w.Style = styleForGlyphSet(glyphs)
	return w
}

// WithAnimation enables or disables frame-driven active-state animation.
//
// Animation is enabled by default.
//
// This is intended for code-level rendering contexts such as stable snapshots,
// not for user-facing configuration.
func (w *Widget) WithAnimation(enabled bool) *Widget {
	w.animation.enabled = enabled
	return w
}

// Err reports cancellation or another rendering error.
func (w *Widget) Err() error {
	return w.err
}

// Init initializes the Bubble Tea model.
func (w *Widget) Init() tea.Cmd {
	w.animation.resetSchedule()
	return w.scheduleAnimationTick()
}

// Update updates the Bubble Tea model.
func (w *Widget) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		w.width = msg.Width
	case Event:
		w.apply(msg)
		return w, w.scheduleAnimationTick()
	case animationTickMsg:
		w.animation.advance(msg)
		return w, w.scheduleAnimationTick()
	case tea.KeyMsg:
		if key.Matches(msg, w.KeyMap.Cancel) {
			w.err = ErrCanceled
			return w, tea.Quit
		}
	}
	return w, nil
}

// View renders the Bubble Tea model.
func (w *Widget) View() tea.View {
	return tea.NewView(w.renderString())
}

func (w *Widget) renderString() string {
	var out strings.Builder
	w.render(&out, w.theme)
	return out.String()
}

func (w *Widget) render(out ui.Writer, theme ui.Theme) {
	var merged, active, pending, failed, skipped int
	for _, item := range w.items {
		switch item.State {
		case StateActive:
			active++
		case StateMerged:
			merged++
		case StateFailed:
			failed++
		case StateSkipped:
			skipped++
		default:
			pending++
		}
	}

	out.WriteString(w.Style.Title.Render(theme, fmt.Sprintf(
		"Merging: %d of %d changes",
		merged, len(w.items),
	)))
	out.WriteString("\n\n")
	w.renderBar(out, theme)
	out.WriteString("\n")
	fmt.Fprintf(out, "merged: %d   waiting: %d   pending: %d",
		merged, active, pending)
	if failed > 0 {
		fmt.Fprintf(out, "   failed: %d", failed)
	}
	if skipped > 0 {
		fmt.Fprintf(out, "   skipped: %d", skipped)
	}
	out.WriteString("\n\n")

	if w.message != "" {
		out.WriteString(w.Style.Detail.Render(theme, w.message))
		out.WriteString("\n")
	}
	out.WriteString(w.Style.Hint.Render(theme,
		"Press Ctrl-C to cancel merge operation."))
}

func (w *Widget) renderBar(out ui.Writer, theme ui.Theme) {
	if len(w.items) == 0 {
		out.WriteString(w.Style.Pending.String(theme))
		return
	}

	width := w.width
	if width <= 0 {
		width = _defaultWidth
	}
	width = max(width, len(w.items))

	segmentWidths := distribute(width, len(w.items))
	for idx, item := range w.items {
		w.renderSegment(out, theme, item.State, segmentWidths[idx])
	}
}

func (w *Widget) renderSegment(
	out ui.Writer,
	theme ui.Theme,
	state State,
	width int,
) {
	if state == StateActive {
		position, glyph, ok := w.animation.marker(width, w.glyphs.Animation)
		if ok {
			active := w.Style.Active.String(theme)
			out.WriteString(strings.Repeat(active, position))
			out.WriteString(w.Style.Active.SetString(glyph).String(theme))
			out.WriteString(strings.Repeat(active, width-position-1))
			return
		}
	}

	out.WriteString(strings.Repeat(
		w.styleFor(state).String(theme),
		width,
	))
}

func (w *Widget) styleFor(state State) ui.Style {
	switch state {
	case StateActive:
		return w.Style.Active
	case StateMerged:
		return w.Style.Merged
	case StateFailed:
		return w.Style.Failed
	case StateSkipped:
		return w.Style.Skipped
	default:
		return w.Style.Pending
	}
}

func (w *Widget) scheduleAnimationTick() tea.Cmd {
	return w.animation.scheduleTick(w.hasActiveItem())
}

func (w *Widget) hasActiveItem() bool {
	for _, item := range w.items {
		if item.State == StateActive {
			return true
		}
	}
	return false
}

// distribute divides a bar width into contiguous item segment widths.
//
// Every segment receives the same base width.
// Any leftover cells are assigned from left to right,
// so the full bar width is used without changing item order.
func distribute(width, segments int) []int {
	if segments <= 0 {
		return nil
	}

	base := width / segments
	extra := width % segments
	widths := make([]int, segments)
	for idx := range widths {
		widths[idx] = base
		if idx < extra {
			widths[idx]++
		}
	}
	return widths
}

func styleForGlyphSet(glyphs GlyphSet) Style {
	return Style{
		Merged: ui.NewStyle().
			Foreground(ui.Green).
			SetString(glyphs.Merged),
		Active: ui.NewStyle().
			Foreground(ui.Yellow).
			SetString(glyphs.Active),
		Pending: ui.NewStyle().
			Foreground(ui.Gray).
			SetString(glyphs.Pending),
		Failed: ui.NewStyle().
			Foreground(ui.Red).
			SetString(glyphs.Failed),
		Skipped: ui.NewStyle().
			Foreground(ui.Plain).
			SetString(glyphs.Skipped),

		Title:   ui.NewStyle().Bold(true),
		Summary: ui.NewStyle().Foreground(ui.Plain),
		Detail:  ui.NewStyle().Foreground(ui.Plain),
		Hint:    ui.NewStyle().Foreground(ui.Gray),
	}
}

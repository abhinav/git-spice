package mergeprogress

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/hexops/autogold/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/ui"
)

func TestWidget_RenderProgression(t *testing.T) {
	widget := New(makeItems(10)...).
		WithTheme(ui.DefaultThemeDark()).
		WithAnimation(false)
	mustUpdate(t, widget, tea.WindowSizeMsg{Width: 40})

	autogold.Expect(`Merging: 0 of 10 changes

□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□
merged: 0   waiting: 0   pending: 10

Press Ctrl-C to cancel merge operation.`).Equal(t, renderPlain(widget))

	mustUpdate(t, widget, Event{
		ItemID: "1",
		State:  StateActive,
	})
	autogold.Expect(`Merging: 0 of 10 changes

◆◆◆◆□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□□
merged: 0   waiting: 1   pending: 9

Press Ctrl-C to cancel merge operation.`).Equal(t, renderPlain(widget))

	mustUpdate(t, widget, Event{
		ItemID: "1",
		State:  StateMerged,
	})
	mustUpdate(t, widget, Event{
		ItemID: "2",
		State:  StateActive,
	})
	mustUpdate(t, widget, Event{
		ItemID: "3",
		State:  StateActive,
	})
	autogold.Expect(`Merging: 1 of 10 changes

■■■■◆◆◆◆◆◆◆◆□□□□□□□□□□□□□□□□□□□□□□□□□□□□
merged: 1   waiting: 2   pending: 7

Press Ctrl-C to cancel merge operation.`).Equal(t, renderPlain(widget))

	mustUpdate(t, widget, Event{
		ItemID: "2",
		State:  StateMerged,
	})
	autogold.Expect(`Merging: 2 of 10 changes

■■■■■■■■◆◆◆◆□□□□□□□□□□□□□□□□□□□□□□□□□□□□
merged: 2   waiting: 1   pending: 7

Press Ctrl-C to cancel merge operation.`).Equal(t, renderPlain(widget))

	mustUpdate(t, widget, Event{
		ItemID: "3",
		State:  StateFailed,
	})
	autogold.Expect(`Merging: 2 of 10 changes

■■■■■■■■××××□□□□□□□□□□□□□□□□□□□□□□□□□□□□
merged: 2   waiting: 0   pending: 7   failed: 1

Press Ctrl-C to cancel merge operation.`).Equal(t, renderPlain(widget))
}

func TestWidget_Render_scalingKeepsShape(t *testing.T) {
	items := makeItems(18)
	for idx := range 7 {
		items[idx].State = StateMerged
	}
	items[7].State = StateActive

	wideWidget := New(items...).
		WithTheme(ui.DefaultThemeDark()).
		WithAnimation(false)
	mustUpdate(t, wideWidget, tea.WindowSizeMsg{Width: 72})
	wide := renderPlain(wideWidget)

	narrowWidget := New(items...).
		WithTheme(ui.DefaultThemeDark()).
		WithAnimation(false)
	mustUpdate(t, narrowWidget, tea.WindowSizeMsg{Width: 24})
	narrow := renderPlain(narrowWidget)

	assert.Contains(t, wide, "Merging: 7 of 18 changes")
	assert.Contains(t, wide, "merged: 7   waiting: 1   pending: 10")
	assert.NotContains(t, barLine(wide), "feature")
	assert.NotContains(t, barLine(narrow), "feature")
	assert.Equal(t, 72, lipgloss.Width(barLine(wide)))
	assert.Equal(t, 24, lipgloss.Width(barLine(narrow)))
	assert.Contains(t, barLine(wide), "■")
	assert.Contains(t, barLine(wide), "◆")
	assert.Contains(t, barLine(wide), "□")
}

func TestWidget_Render_failedAndSkipped(t *testing.T) {
	items := makeItems(5)
	items[0].State = StateMerged
	items[1].State = StateSkipped
	items[2].State = StateActive
	items[3].State = StateFailed

	widget := New(items...).
		WithTheme(ui.DefaultThemeDark()).
		WithAnimation(false)
	mustUpdate(t, widget, tea.WindowSizeMsg{Width: 20})
	got := renderPlain(widget)

	assert.Contains(t, got, "merged: 1   waiting: 1   pending: 1")
	assert.Contains(t, got, "failed: 1")
	assert.Contains(t, got, "skipped: 1")
	assert.Equal(t, "■■■■○○○○◆◆◆◆××××□□□□", barLine(got))
}

func TestWidget_Render_ASCIIGlyphSet(t *testing.T) {
	items := makeItems(5)
	items[0].State = StateMerged
	items[1].State = StateSkipped
	items[2].State = StateActive
	items[3].State = StateFailed

	widget := New(items...).
		WithTheme(ui.DefaultThemeDark()).
		WithGlyphSet(ASCIIGlyphSet()).
		WithAnimation(false)
	mustUpdate(t, widget, tea.WindowSizeMsg{Width: 20})

	assert.Equal(t, "####oooo====xxxx----", barLine(renderPlain(widget)))
}

func renderPlain(widget *Widget) string {
	return ansi.Strip(widget.View().Content)
}

func mustUpdate(t *testing.T, widget *Widget, msg tea.Msg) {
	t.Helper()

	model, cmd := widget.Update(msg)
	require.Nil(t, cmd)
	require.Same(t, widget, model)
}

func barLine(rendered string) string {
	lines := strings.Split(rendered, "\n")
	return lines[2]
}

func makeItems(n int) []Item {
	items := make([]Item, n)
	for idx := range items {
		items[idx] = Item{
			ID: strconv.Itoa(idx + 1),
		}
	}
	return items
}

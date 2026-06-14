package scrollregion

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hexops/autogold/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vito/midterm"
)

func TestModel_RendersWithExplicitSize(t *testing.T) {
	var buf bytes.Buffer
	model := New(&testModel{}, &buf, &Options{
		Width:     40,
		Height:    10,
		MinHeight: 2,
		MaxHeight: 3,
	})

	_, cmd := model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	require.Nil(t, cmd)
	_, cmd = model.Update(testStep{})
	require.NotNil(t, cmd)

	got := visibleControl(buf.String())
	assert.Contains(t, got, "<esc>[1;8r")              // set scroll margins
	assert.Contains(t, got, "<esc>[10;1Hview: 0 40x2") // draw bottom row
	assert.Contains(t, got, "<esc>[10;7H1 ")           // diff changed cell
	assert.NotContains(t, got, "<esc>[10;1H<esc>[2Kview: 1 40x2")

	assert.NoError(t, model.Close())
	got = visibleControl(buf.String())
	assert.Contains(t, got, "<esc>[r") // reset scroll margins
}

func TestModel_GrowsToMax(t *testing.T) {
	var buf bytes.Buffer
	model := New(&tallTestModel{}, &buf, &Options{
		Width:     40,
		Height:    10,
		MinHeight: 2,
		MaxHeight: 4,
	})

	_, cmd := model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	require.Nil(t, cmd)
	_, cmd = model.Update(testStep{})
	require.NotNil(t, cmd)

	got := visibleControl(buf.String())
	assert.Contains(t, got, "<esc>[1;8r")                 // reserve two bottom rows
	assert.Contains(t, got, "<esc>[1;6r")                 // grow to four bottom rows
	assert.Contains(t, got, "<esc>[7;1Hline 1")           // draw top reserved row
	assert.Contains(t, got, "line 3\r\nline 4<esc>[6;1H") // draw bottom reserved row
}

func TestModel_RendersAfterFirstWindowSize(t *testing.T) {
	var buf bytes.Buffer
	model := New(&staticTestModel{}, &buf, &Options{
		Width:     40,
		MinHeight: 2,
		MaxHeight: 3,
	})

	_, cmd := model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	require.Nil(t, cmd)

	got := visibleControl(buf.String())
	assert.Contains(t, got, "<esc>[1;8r")
	assert.Contains(t, got, "<esc>[10;1Hwidget")
}

func TestModel_PreservesStyledContent(t *testing.T) {
	t.Setenv("TTY_FORCE", "1")
	t.Setenv("NO_COLOR", "false")

	var buf bytes.Buffer
	model := New(&styledTestModel{}, &buf, &Options{
		Width:     40,
		Height:    10,
		MinHeight: 2,
		MaxHeight: 3,
		TERM:      "xterm-256color",
	})

	_, cmd := model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	require.Nil(t, cmd)

	got := visibleControl(buf.String())
	assert.Contains(t, got, "<esc>[31mred")
}

func TestRenderer_LogsScrollAboveRegion(t *testing.T) {
	term := midterm.NewTerminal(10, 40)
	term.Raw = true

	model := New(&staticTestModel{}, term, &Options{
		Width:     40,
		Height:    10,
		MinHeight: 3,
		MaxHeight: 3,
	})
	_, _ = model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})

	_, err := io.WriteString(term, "log 1\r\nlog 2\r\nlog 3\r\nlog 4\r\n")
	require.NoError(t, err)

	rows := renderedRows(term)
	assert.True(t, rowContains(rows[:7], "log 4"), rows[:7])
	assert.Equal(t, "widget", rows[9])

	require.NoError(t, model.Close())
}

func TestRenderer_RenderedRows(t *testing.T) {
	term := midterm.NewTerminal(10, 40)
	term.Raw = true

	model := New(&rectangleTestModel{}, term, &Options{
		Width:     40,
		Height:    10,
		MinHeight: 4,
		MaxHeight: 6,
	})
	_, _ = model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})

	_, err := io.WriteString(term, "pulled 1 commit\r\nupdated #123\r\n")
	require.NoError(t, err)

	autogold.Expect([]string{
		"",
		"",
		"",
		"pulled 1 commit",
		"updated #123",
		"",
		"+--------------+",
		"|              |",
		"|              |",
		"+--------------+",
	}).Equal(t, renderedRows(term))

	require.NoError(t, model.Close())
}

type testModel struct {
	step   int
	width  int
	height int
}

func (m *testModel) Init() tea.Cmd {
	return func() tea.Msg { return testStep{} }
}

func (m *testModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case testStep:
		m.step++
		return m, tea.Quit
	}
	return m, nil
}

func (m *testModel) View() tea.View {
	return tea.NewView(fmt.Sprintf("view: %d %dx%d", m.step, m.width, m.height))
}

type testStep struct{}

type staticTestModel struct{}

func (m *staticTestModel) Init() tea.Cmd {
	return nil
}

func (m *staticTestModel) Update(tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m *staticTestModel) View() tea.View {
	return tea.NewView("widget")
}

type styledTestModel struct{}

func (m *styledTestModel) Init() tea.Cmd {
	return nil
}

func (m *styledTestModel) Update(tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m *styledTestModel) View() tea.View {
	return tea.NewView("\x1b[31mred\x1b[0m")
}

type tallTestModel struct {
	tall bool
}

func (m *tallTestModel) Init() tea.Cmd {
	return func() tea.Msg { return testStep{} }
}

func (m *tallTestModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(testStep); ok {
		m.tall = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *tallTestModel) View() tea.View {
	if !m.tall {
		return tea.NewView("short")
	}
	return tea.NewView("line 1\nline 2\nline 3\nline 4\nline 5")
}

type rectangleTestModel struct {
	width  int
	height int
}

func (m *rectangleTestModel) Init() tea.Cmd {
	return nil
}

func (m *rectangleTestModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = min(size.Width, 16)
		m.height = size.Height
	}
	return m, nil
}

func (m *rectangleTestModel) View() tea.View {
	width := max(2, m.width)
	height := max(2, m.height)
	top := "+" + strings.Repeat("-", width-2) + "+"
	middle := "|" + strings.Repeat(" ", width-2) + "|"

	lines := make([]string, height)
	lines[0] = top
	for i := 1; i < height-1; i++ {
		lines[i] = middle
	}
	lines[height-1] = top
	return tea.NewView(strings.Join(lines, "\n"))
}

func visibleControl(s string) string {
	return strings.ReplaceAll(s, "\x1b", "<esc>")
}

func renderedRows(term *midterm.Terminal) []string {
	rows := make([]string, len(term.Content))
	for i, row := range term.Content {
		rows[i] = string(trimRightWS(row))
	}
	return rows
}

func rowContains(rows []string, want string) bool {
	return slices.ContainsFunc(rows, func(row string) bool {
		return strings.Contains(row, want)
	})
}

func trimRightWS(rs []rune) []rune {
	for i, v := range slices.Backward(rs) {
		switch v {
		case 0, ' ', '\t', '\n':
		default:
			rs = rs[:i+1]
			if j := slices.Index(rs, 0); j >= 0 {
				rs = slices.Clone(rs)
				for k := j; k < len(rs); k++ {
					if rs[k] == 0 {
						rs[k] = ' '
					}
				}
			}
			return rs
		}
	}
	return nil
}

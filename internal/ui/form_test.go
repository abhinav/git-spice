package ui_test

import (
	"bytes"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/ui"
)

func TestRunModel_colorProfile(t *testing.T) {
	tests := []struct {
		name       string
		profile    colorprofile.Profile
		wantColor  bool
		wantEscape bool
	}{
		{name: "Color", profile: colorprofile.TrueColor, wantColor: true, wantEscape: true},
		{name: "Plain", profile: colorprofile.ASCII, wantEscape: true},
		{name: "NoTTY", profile: colorprofile.NoTTY},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			err := ui.RunModel(new(coloredModel), &ui.RunOptions{
				Input: bytes.NewReader(nil),
				Output: &colorprofile.Writer{
					Forward: &output,
					Profile: tt.profile,
				},
				Width:          40,
				Height:         8,
				TERM:           "xterm-256color",
				WithoutSignals: true,
			})
			require.NoError(t, err)
			assert.Contains(t, output.String(), "model frame")
			assert.Equal(t, tt.wantColor, strings.Contains(output.String(), "\x1b[31m"))
			assert.Equal(t, tt.wantEscape, strings.Contains(output.String(), "\x1b"))
		})
	}
}

func TestForm_Footer(t *testing.T) {
	field := &footerField{
		view:   "field value",
		footer: "clear footer",
	}
	form := ui.NewForm(field)

	assert.Contains(t, form.Render(), "clear footer")

	_, _ = form.Update(ui.AcceptField())

	assert.NotContains(t, form.Render(), "clear footer")
	assert.Contains(t, form.Render(), "field value")
}

func TestForm_FooterWraps(t *testing.T) {
	field := &footerField{
		view: "field value",
		footer: "Fork mode: local stacks work normally, but submit creates " +
			"CRs only for trunk-based branches.",
	}
	form := ui.NewForm(field)
	_, _ = form.Update(tea.WindowSizeMsg{Width: 36, Height: 10})

	rendered := form.Render()
	assert.Contains(t, rendered, "field value\n\nFork mode: local stacks work")
	assert.Contains(t, rendered, "\nnormally, but submit creates CRs")
	assert.LessOrEqual(t, maxLineWidth(rendered), 36)
}

type coloredModel struct{}

func (*coloredModel) Init() tea.Cmd { return nil }

func (m *coloredModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.ColorProfileMsg); ok {
		return m, tea.Quit
	}
	return m, nil
}

func (*coloredModel) View() tea.View {
	return tea.NewView("\x1b[31mmodel frame\x1b[m")
}

type footerField struct {
	view   string
	footer string
}

var _ ui.Field = (*footerField)(nil)

func (*footerField) Init() tea.Cmd { return nil }

func (*footerField) Update(tea.Msg) tea.Cmd { return nil }

func (f *footerField) Render(w ui.Writer, _ ui.Theme) {
	_, _ = w.WriteString(f.view)
}

func (*footerField) UnmarshalValue(func(any) error) error { return nil }

func (*footerField) Err() error { return nil }

func (*footerField) Title() string { return "" }

func (*footerField) Description() string { return "" }

func (f *footerField) Footer() string {
	return f.footer
}

func maxLineWidth(s string) int {
	maxWidth := 0
	for line := range strings.SplitSeq(s, "\n") {
		maxWidth = max(maxWidth, len(line))
	}
	return maxWidth
}

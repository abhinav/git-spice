package ui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/ui"
)

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

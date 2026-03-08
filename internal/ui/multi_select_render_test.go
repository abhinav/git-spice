package ui

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMultiSelect_Render_nilStyleUsesDefault(t *testing.T) {
	var buf bytes.Buffer
	field := NewMultiSelect(func(w Writer, _ Theme, _ int, opt MultiSelectOption[string]) {
		w.WriteString(opt.Value)
	}).WithOptions(
		MultiSelectOption[string]{Value: "alpha"},
	)
	field.visible = 5

	field.Render(&buf, DefaultThemeLight())

	assert.Contains(t, buf.String(), "▶")
	assert.Contains(t, buf.String(), "Done")
}

func TestMultiSelect_Render_exactStyle(t *testing.T) {
	var buf bytes.Buffer
	field := NewMultiSelect(func(w Writer, _ Theme, _ int, opt MultiSelectOption[string]) {
		w.WriteString(opt.Value)
	}).WithOptions(
		MultiSelectOption[string]{Value: "alpha"},
	)
	field.Style = MultiSelectStyle{}
	field.visible = 5

	field.Render(&buf, DefaultThemeLight())

	assert.Equal(t, "\n alpha\n ", buf.String())
}

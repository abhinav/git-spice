package ui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/ui"
)

func TestConfirm_accept(t *testing.T) {
	t.Run("default/false", func(t *testing.T) {
		c := ui.NewConfirm()
		c.Update(tea.KeyMsg{Type: tea.KeyEnter})
		assert.False(t, c.Value())
	})

	t.Run("default/true", func(t *testing.T) {
		value := true
		c := ui.NewConfirm().WithValue(&value)
		c.Update(tea.KeyMsg{Type: tea.KeyEnter})

		assert.True(t, c.Value())
		assert.True(t, value)
	})

	t.Run("yes", func(t *testing.T) {
		c := ui.NewConfirm()
		c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
		assert.True(t, c.Value())
	})

	t.Run("no", func(t *testing.T) {
		c := ui.NewConfirm()
		c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
		assert.False(t, c.Value())
	})
}

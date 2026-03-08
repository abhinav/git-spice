package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Color identifies a color in the active UI theme.
type Color uint8

// Theme color identifiers used by UI styles.
const (
	// Unset leaves the corresponding color unchanged.
	Unset Color = iota
	Yellow
	Red
	Green
	Plain
	Cyan
	Magenta
	Gray
)

// Resolve resolves the color against the active theme.
func (c Color) Resolve(theme Theme) color.Color {
	switch c {
	case Yellow:
		return theme.Yellow
	case Red:
		return theme.Red
	case Green:
		return theme.Green
	case Plain:
		return theme.Plain
	case Cyan:
		return theme.Cyan
	case Magenta:
		return theme.Magenta
	case Gray:
		return theme.Gray
	default:
		return nil
	}
}

// Style describes UI styling independent of the active theme.
// A Style is resolved to a lipgloss style at render time.
// Zero value of Style is a plain style with no colors or attributes.
// Style may be copied by value.
type Style struct {
	foreground Color
	background Color

	bold      bool
	faint     bool
	underline bool

	value string
}

// NewStyle builds an empty style.
func NewStyle() Style {
	return Style{}
}

// Foreground returns a copy of the style with the given foreground color.
func (s Style) Foreground(c Color) Style {
	s.foreground = c
	return s
}

// Background returns a copy of the style with the given background color.
func (s Style) Background(c Color) Style {
	s.background = c
	return s
}

// Bold returns a copy of the style with the given bold setting.
func (s Style) Bold(v bool) Style {
	s.bold = v
	return s
}

// Faint returns a copy of the style with the given faint setting.
func (s Style) Faint(v bool) Style {
	s.faint = v
	return s
}

// Underline returns a copy of the style with the given underline setting.
func (s Style) Underline(v bool) Style {
	s.underline = v
	return s
}

// SetString returns a copy of the style with the given string value.
func (s Style) SetString(v string) Style {
	s.value = v
	return s
}

// Resolve resolves the style against the active theme.
func (s Style) Resolve(theme Theme) lipgloss.Style {
	style := lipgloss.NewStyle().
		Bold(s.bold).
		Faint(s.faint).
		Underline(s.underline)

	if s.foreground != Unset {
		style = style.Foreground(s.foreground.Resolve(theme))
	}
	if s.background != Unset {
		style = style.Background(s.background.Resolve(theme))
	}
	if s.value != "" {
		style = style.SetString(s.value)
	}

	return style
}

// Render renders the given string with the resolved style.
func (s Style) Render(theme Theme, v string) string {
	return s.Resolve(theme).Render(v)
}

// String renders the style's string value.
func (s Style) String(theme Theme) string {
	return s.Resolve(theme).String()
}

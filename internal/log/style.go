package log

import (
	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/ui"
)

// Style defines the output styling for the logger.
type Style struct {
	Key lipgloss.Style

	KeyValueDelimiter lipgloss.Style          // required
	LevelLabels       ByLevel[lipgloss.Style] // required
	MultilinePrefix   lipgloss.Style          // required
	PrefixDelimiter   lipgloss.Style          // required

	Messages ByLevel[lipgloss.Style]
	Values   map[string]lipgloss.Style
}

// DefaultStyle returns the default style for the logger.
func DefaultStyle() *Style {
	return &Style{
		Key:               ui.NewStyle().Faint(true),
		KeyValueDelimiter: ui.NewStyle().SetString("=").Faint(true),
		MultilinePrefix:   ui.NewStyle().SetString("| ").Faint(true),
		PrefixDelimiter:   ui.NewStyle().SetString(": "),
		LevelLabels: ByLevel[lipgloss.Style]{
			Trace: ui.NewStyle().SetString("TRC").Foreground(lipgloss.Color("8")),  // gray
			Debug: ui.NewStyle().SetString("DBG"),                                  // default
			Info:  ui.NewStyle().SetString("INF").Foreground(lipgloss.Color("10")), // green
			Warn:  ui.NewStyle().SetString("WRN").Foreground(lipgloss.Color("11")), // yellow
			Error: ui.NewStyle().SetString("ERR").Foreground(lipgloss.Color("9")),  // red
			Fatal: ui.NewStyle().SetString("FTL").Foreground(lipgloss.Color("9")),  // red
		},
		Messages: ByLevel[lipgloss.Style]{
			Trace: ui.NewStyle().Foreground(lipgloss.Color("8")), // gray
			Debug: ui.NewStyle().Faint(true),
			Info:  ui.NewStyle().Bold(true),
			Warn:  ui.NewStyle().Bold(true),
			Error: ui.NewStyle().Bold(true),
			Fatal: ui.NewStyle().Bold(true),
		},
		Values: map[string]lipgloss.Style{
			"error": ui.NewStyle().Foreground(lipgloss.Color("9")), // red
		},
	}
}

// PlainStyle returns a style for the logger without any colors.
func PlainStyle() *Style {
	return &Style{
		KeyValueDelimiter: ui.NewStyle().SetString("="),
		MultilinePrefix:   ui.NewStyle().SetString("  | "),
		PrefixDelimiter:   ui.NewStyle().SetString(": "),
		LevelLabels: ByLevel[lipgloss.Style]{
			Trace: ui.NewStyle().SetString("TRC"),
			Debug: ui.NewStyle().SetString("DBG"),
			Info:  ui.NewStyle().SetString("INF"),
			Warn:  ui.NewStyle().SetString("WRN"),
			Error: ui.NewStyle().SetString("ERR"),
			Fatal: ui.NewStyle().SetString("FTL"),
		},
	}
}

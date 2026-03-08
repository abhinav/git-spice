package ui

import (
	"image/color"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
)

// Theme describes the current terminal color theme.
//
// It defines a mapping from [Color] to colors that are used
// at render time.
type Theme struct {
	Yellow  color.Color
	Red     color.Color
	Green   color.Color
	Plain   color.Color
	Cyan    color.Color
	Magenta color.Color
	Gray    color.Color
}

var defaultLightTheme = Theme{
	Yellow:  lipgloss.Color("2"),
	Red:     lipgloss.Color("1"),
	Green:   lipgloss.Color("2"),
	Plain:   lipgloss.Color("0"),
	Cyan:    lipgloss.Color("6"),
	Magenta: lipgloss.Color("5"),
	Gray:    lipgloss.Color("8"),
}

var defaultDarkTheme = Theme{
	Yellow:  lipgloss.Color("11"),
	Red:     lipgloss.Color("9"),
	Green:   lipgloss.Color("10"),
	Plain:   lipgloss.Color("7"),
	Cyan:    lipgloss.Color("14"),
	Magenta: lipgloss.Color("13"),
	Gray:    lipgloss.Color("8"),
}

// DefaultThemeLight returns the default light-terminal theme.
func DefaultThemeLight() Theme { return defaultLightTheme }

// DefaultThemeDark returns the default dark-terminal theme.
func DefaultThemeDark() Theme { return defaultDarkTheme }

// detectTheme detects the current terminal theme.
//
// TODO: Allow users to inject theme at startup.
func detectTheme(in io.Reader, out io.Writer) Theme {
	switch strings.ToLower(os.Getenv("__GIT_SPICE_THEME")) {
	case "light":
		return DefaultThemeLight()
	case "dark":
		return DefaultThemeDark()
	}

	inFile, okIn := in.(term.File)
	outFile, okOut := out.(term.File)
	if okIn && okOut {
		if lipgloss.HasDarkBackground(inFile, outFile) {
			return DefaultThemeDark()
		}

		return DefaultThemeLight()
	}

	return DefaultThemeDark()
}

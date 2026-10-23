package main

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/mattn/go-isatty"
	"go.abhg.dev/gs/internal/git"
)

// colorPreference is an explicit user choice
// about colored output, or the lack of one.
type colorPreference int

const (
	// colorUnset means no explicit preference;
	// colored output follows terminal detection.
	colorUnset colorPreference = iota

	// colorOn requests colored output.
	colorOn

	// colorOff disables colored output.
	colorOff
)

// parseColorValue interprets a git configuration value
// for color.ui or spice.color.
//
// Besides git's booleans, both accept the color-specific
// values "always" (on), "never" (off), and "auto" (unset),
// matching git's own color.ui semantics.
func parseColorValue(value string) colorPreference {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "on", "1", "always":
		return colorOn
	case "false", "no", "off", "0", "never":
		return colorOff
	default:
		// "auto", an empty value, or anything unrecognized
		// leaves the decision to terminal detection.
		return colorUnset
	}
}

// resolveColorPreference reports the user's color preference
// from git configuration.
//
// It reads the standard color.ui and the git-spice specific
// spice.color; spice.color wins when both are set.
// An unreadable configuration means no preference,
// so a broken git installation never changes startup behavior.
func resolveColorPreference(ctx context.Context, cfg *git.Config) colorPreference {
	var ui, spice colorPreference
	for entry, err := range cfg.ListRegexp(ctx, `^color\.ui$`, `^spice\.color$`) {
		if err != nil {
			continue
		}
		switch entry.Key.Canonical() {
		case "color.ui":
			ui = parseColorValue(entry.Value)
		case "spice.color":
			spice = parseColorValue(entry.Value)
		}
	}
	if spice != colorUnset {
		return spice
	}
	return ui
}

// plainColorProfile is the color profile to use
// when the user has disabled colored output.
// If the output is a terminal, colors are removed
// but control sequences are kept intact for interactive use.
// Otherwise, all escape sequences are stripped
// for plain-text output.
func plainColorProfile(w io.Writer) colorprofile.Profile {
	if f, ok := w.(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		return colorprofile.Ascii
	}
	return colorprofile.NoTTY
}

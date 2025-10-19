package ui_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/uitest"
)

// Expected files:
//
//   - want: expected string value
//   - give (optional): starting string value
//   - desc (optional): description of prompt
//   - invalid (optional): value rejected as invalid
//   - options (optional): newline-separated list of options
func TestInput(t *testing.T) {
	uitest.RunScripts(t,
		func(t testing.TB, ts *testscript.TestScript, view ui.InteractiveView) {
			want := strings.TrimSpace(ts.ReadFile("want"))

			var give string
			if _, err := os.Stat(ts.MkAbs("give")); err == nil {
				give = strings.TrimSpace(ts.ReadFile("give"))
			}

			var desc string
			if _, err := os.Stat(ts.MkAbs("desc")); err == nil {
				desc = strings.TrimSpace(ts.ReadFile("desc"))
			}

			var invalid string
			if _, err := os.Stat(ts.MkAbs("invalid")); err == nil {
				invalid = strings.TrimSpace(ts.ReadFile("invalid"))
			}

			var options []string
			if _, err := os.Stat(ts.MkAbs("options")); err == nil {
				contents := strings.TrimSpace(ts.ReadFile("options"))
				if contents != "" {
					options = strings.Split(contents, "\n")
				}
			}

			got := give
			widget := ui.NewInput().
				WithTitle("Please answer").
				WithValue(&got).
				WithValidate(func(s string) error {
					if s == invalid {
						return errors.New("invalid value")
					}
					return nil
				}).
				WithDescription(desc)

			if len(options) > 0 {
				widget = widget.WithOptions(options)
			}

			require.NoError(t, ui.Run(view, widget))
			assert.Equal(t, want, got)
		},
		&uitest.RunScriptsOptions{
			Update: *ui.UpdateFixtures,
		},
		"testdata/script/input",
	)
}

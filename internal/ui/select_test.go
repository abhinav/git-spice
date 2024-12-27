package ui_test

import (
	"encoding/json"
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
//   - give: []string options
//   - selected (optional): starting string value
//   - visible (optional): number of items visible at a time
func TestSelect(t *testing.T) {
	uitest.RunScripts(t,
		func(t testing.TB, ts *testscript.TestScript, view ui.InteractiveView) {
			var want string
			require.NoError(t,
				json.Unmarshal([]byte(ts.ReadFile("want")), &want),
				"read 'want' file")

			var give []string
			require.NoError(t,
				json.Unmarshal([]byte(ts.ReadFile("give")), &give),
				"read 'give' file")

			var desc string
			if _, err := os.Stat(ts.MkAbs("desc")); err == nil {
				desc = strings.TrimSpace(ts.ReadFile("desc"))
			}

			var visible int
			if _, err := os.Stat(ts.MkAbs("visible")); err == nil {
				require.NoError(t,
					json.Unmarshal([]byte(ts.ReadFile("visible")), &visible),
					"read 'visible' file")
			}

			var selected string
			if _, err := os.Stat(ts.MkAbs("selected")); err == nil {
				require.NoError(t,
					json.Unmarshal([]byte(ts.ReadFile("selected")), &selected),
					"read 'selected' file")
			}

			var got string
			widget := ui.NewSelect[string]().
				WithTitle("Pick a value").
				WithValue(&got).
				WithDescription(desc).
				WithVisible(visible).
				With(ui.ComparableOptions(selected, give...))

			require.NoError(t, ui.Run(view, widget))
			assert.Equal(t, want, got)
		},
		&uitest.RunScriptsOptions{
			Update: *ui.UpdateFixtures,
		},
		"testdata/script/select",
	)
}

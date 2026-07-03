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
//   - want: expected boolean value
//   - give (optional): starting boolean value
//   - desc (optional): description of prompt
func TestConfirm(t *testing.T) {
	uitest.RunScripts(t,
		func(t testing.TB, ts *testscript.TestScript, view ui.InteractiveView) {
			var give bool
			if _, err := os.Stat(ts.MkAbs("give")); err == nil {
				require.NoError(t,
					json.Unmarshal([]byte(ts.ReadFile("give")), &give),
					"read 'give' file")
			}

			var want bool
			require.NoError(t,
				json.Unmarshal([]byte(ts.ReadFile("want")), &want),
				"read 'want' file")

			var desc string
			if _, err := os.Stat(ts.MkAbs("desc")); err == nil {
				desc = strings.TrimSpace(ts.ReadFile("desc"))
			}

			got := give
			widget := ui.NewConfirm().
				WithTitle("Yes or no?").
				WithValue(&got).
				WithDescription(desc)

			require.NoError(t, ui.Run(view, widget))
			assert.Equal(t, want, got)
		},
		&uitest.RunScriptsOptions{
			Update: ui.UpdateFixtures(),
		},
		"testdata/script/confirm",
	)
}

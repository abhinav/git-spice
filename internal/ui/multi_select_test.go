package ui_test

import (
	"encoding/json"
	"os"
	"slices"
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
//   - want: expected []string values
//   - give: available []string options
//   - selected (optional): []string values already selected
//   - desc (optional): widget description
func TestMultiSelect(t *testing.T) {
	uitest.RunScripts(t,
		func(t testing.TB, ts *testscript.TestScript, view ui.InteractiveView) {
			var want []string
			require.NoError(t,
				json.Unmarshal([]byte(ts.ReadFile("want")), &want),
				"read 'want' file")

			var selected []string
			if _, err := os.Stat(ts.MkAbs("selected")); err == nil {
				require.NoError(t,
					json.Unmarshal([]byte(ts.ReadFile("selected")), &selected),
					"read 'selected' file")
			}

			var give []string
			require.NoError(t,
				json.Unmarshal([]byte(ts.ReadFile("give")), &give),
				"read 'give' file")

			options := make([]ui.MultiSelectOption[string], len(give))
			for i, value := range give {
				options[i] = ui.MultiSelectOption[string]{
					Value:    value,
					Selected: slices.Contains(selected, value),
				}
			}

			var desc string
			if _, err := os.Stat(ts.MkAbs("desc")); err == nil {
				desc = strings.TrimSpace(ts.ReadFile("desc"))
			}

			widget := ui.NewMultiSelect[string](func(w ui.Writer, i int, opt ui.MultiSelectOption[string]) {
				if opt.Selected {
					w.WriteString("[X] ")
				} else {
					w.WriteString("[ ] ")
				}
				w.WriteString(opt.Value)
			}).
				WithTitle("Pick one or more").
				WithDescription(desc).
				WithOptions(options...)

			require.NoError(t, ui.Run(view, widget))

			var got []string
			for _, value := range widget.Selected() {
				got = append(got, give[value])
			}
			assert.Equal(t, want, got)
		},
		&uitest.RunScriptsOptions{
			Update: *ui.UpdateFixtures,
		},
		"testdata/script/multi_select",
	)
}

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
//   - give: []listItem options
//   - selected (optional): starting value that will be selected
//   - want: expected final string value
//   - desc (optional): field description
//
// listItem has the schema:
//
//	{
//		"value": string,        // text to display and resulting value
//		"desc": string?,        // description of the entry (if any)
//		"focusedDesc": string?, // different description to display for focused entries
//	}
func TestList(t *testing.T) {
	type listItem struct {
		Value       string `json:"value"`
		Desc        string `json:"desc"`
		FocusedDesc string `json:"focusedDesc"`
	}

	uitest.RunScripts(t,
		func(t testing.TB, ts *testscript.TestScript, view ui.InteractiveView) {
			var give []listItem
			require.NoError(t,
				json.Unmarshal([]byte(ts.ReadFile("give")), &give),
				"read 'give' file")

			want := strings.TrimSpace(ts.ReadFile("want"))

			listItems := make([]ui.ListItem[string], len(give))
			for i, item := range give {
				listItems[i] = ui.ListItem[string]{
					Title: item.Value,
					Value: item.Value,
					Description: func(focused bool) string {
						desc := item.Desc
						if focused && item.FocusedDesc != "" {
							desc = item.FocusedDesc
						}
						return desc
					},
				}
			}

			var desc string
			if _, err := os.Stat(ts.MkAbs("desc")); err == nil {
				desc = strings.TrimSpace(ts.ReadFile("desc"))
			}

			var selectedIdx int
			if _, err := os.Stat(ts.MkAbs("selected")); err == nil {
				selected := strings.TrimSpace(ts.ReadFile("selected"))
				selectedIdx = slices.IndexFunc(give, func(item listItem) bool {
					return item.Value == selected
				})
				if selectedIdx == -1 {
					t.Fatalf("Bad 'selected' file: %q not found", selected)
				}
			}

			var got string
			widget := ui.NewList[string]().
				WithTitle("Pick an item").
				WithValue(&got).
				WithDescription(desc).
				WithSelected(selectedIdx).
				WithItems(listItems...)

			require.NoError(t, ui.Run(view, widget))
			assert.Equal(t, want, got)
		},
		&uitest.RunScriptsOptions{
			Update: *ui.UpdateFixtures,
		},
		"testdata/script/list",
	)
}

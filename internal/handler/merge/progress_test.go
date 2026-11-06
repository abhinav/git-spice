package merge

import (
	"io"
	"testing"
	"testing/synctest"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget/mergeprogress"
)

func TestWidgetMergeProgress_FinishAfterRunnerExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		view := new(earlyExitModelView)
		progress := newWidgetMergeProgress(view, ui.DefaultThemeLight())
		progress.Start(t.Context(), []*mergeItem{{
			branch: "feat1",
		}})

		synctest.Wait()
		select {
		case <-progress.stopped:
		default:
			t.Fatal("progress model did not exit")
		}

		done := make(chan error, 1)
		go func() {
			progress.Event(mergeProgressEvent{
				Kind: mergeProgressMergeabilityFailed,
				Item: &mergeItem{
					branch: "feat1",
				},
			})
			done <- progress.Finish()
		}()

		synctest.Wait()
		select {
		case err := <-done:
			require.NoError(t, err)
		default:
			t.Fatal("Finish blocked after progress model exited")
		}
	})
}

func TestMergeProgressItems_ordersByDependencyLayer(t *testing.T) {
	got := mergeProgressItems([]*mergeItem{
		{branch: "D", base: "B"},
		{branch: "Y", base: "X"},
		{branch: "B", base: "A"},
		{branch: "A", base: "main"},
		{branch: "C", base: "A"},
		{branch: "X", base: "main"},
	})

	assert.Equal(t, []string{
		"A",
		"X",
		"B",
		"C",
		"Y",
		"D",
	}, progressItemIDs(got))
}

func (*earlyExitModelView) Write(p []byte) (int, error) {
	return io.Discard.Write(p)
}

func (*earlyExitModelView) Theme() ui.Theme {
	return ui.DefaultThemeLight()
}

func (v *earlyExitModelView) RunModel(_ tea.Model, opts *ui.RunOptions) error {
	v.opts = opts
	return nil
}

type earlyExitModelView struct {
	opts *ui.RunOptions
}

func progressItemIDs(items []mergeprogress.Item) []string {
	ids := make([]string, len(items))
	for idx, item := range items {
		ids[idx] = item.ID
	}
	return ids
}

package merge

import (
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"go.abhg.dev/gs/internal/ui"
)

func TestWidgetMergeProgress_FinishAfterRunnerExit(t *testing.T) {
	progress := newWidgetMergeProgress(
		earlyExitModelView{},
		ui.DefaultThemeLight(),
	)
	progress.Start(t.Context(), []*mergeItem{{
		branch: "feat1",
	}})

	select {
	case <-progress.stopped:
	case <-time.After(time.Second):
		t.Fatal("progress model did not exit")
	}

	done := make(chan error, 1)
	go func() {
		progress.Event(mergeProgressEvent{
			Kind: mergeProgressChecksFailed,
			Item: &mergeItem{
				branch: "feat1",
			},
		})
		done <- progress.Finish()
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Finish blocked after progress model exited")
	}
}

type earlyExitModelView struct{}

func (earlyExitModelView) Write(p []byte) (int, error) {
	return io.Discard.Write(p)
}

func (earlyExitModelView) Theme() ui.Theme {
	return ui.DefaultThemeLight()
}

func (earlyExitModelView) RunModel(tea.Model, *ui.RunOptions) error {
	return nil
}

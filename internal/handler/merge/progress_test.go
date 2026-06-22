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
	view := new(earlyExitModelView)
	progress := newWidgetMergeProgress(view, ui.DefaultThemeLight())
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
			Kind: mergeProgressMergeabilityFailed,
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

package merge

import (
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget/mergeprogress"
)

// mergeProgress reports merge-loop progress to either the terminal widget
// or the non-interactive log stream.
type mergeProgress interface {
	Event(mergeProgressEvent)
}

// mergeProgressEvent is one progress event emitted by the merge loop.
type mergeProgressEvent struct {
	// Kind identifies the merge-loop state being reported.
	Kind mergeProgressEventKind

	// Item identifies the branch and change being updated.
	Item *mergeItem

	// URL is the forge URL associated with merge request events.
	URL string

	// Base is the target branch associated with retargeting events.
	Base string
}

// mergeProgressEventKind identifies the operation state being reported.
type mergeProgressEventKind uint8

const (
	mergeProgressPreparing              mergeProgressEventKind = iota // branch preparation started
	mergeProgressPrepareFailed                                        // local preparation failed
	mergeProgressRetargeting                                          // retargeting to trunk started
	mergeProgressRetargetFailed                                       // retargeting to trunk failed
	mergeProgressWaitingForForgeHead                                  // forge HEAD still stale
	mergeProgressForgeHeadFailed                                      // forge HEAD did not update
	mergeProgressWaitingForMergeability                               // merge readiness still pending
	mergeProgressMergeabilityReady                                    // ready to merge
	mergeProgressMergeabilityFailed                                   // merge readiness blocked or timed out
	mergeProgressMerging                                              // forge merge request started
	mergeProgressMergeFailed                                          // forge merge request failed
	mergeProgressMergeRequested                                       // merge requested without waiting
	mergeProgressWaitingForMerge                                      // waiting for merged state
	mergeProgressMergeIncomplete                                      // merged state did not appear
	mergeProgressMerged                                               // merged state observed
	mergeProgressSyncFailed                                           // trunk sync failed
	mergeProgressFailed                                               // branch failed by scheduler policy
	mergeProgressSkipped                                              // branch skipped by scheduler policy
)

// logMergeProgress reports merge progress through sparse log messages.
type logMergeProgress struct {
	log *silog.Logger

	last map[string]mergeProgressEvent
}

func newLogMergeProgress(log *silog.Logger) *logMergeProgress {
	return &logMergeProgress{
		log:  log,
		last: make(map[string]mergeProgressEvent),
	}
}

func (p *logMergeProgress) Event(event mergeProgressEvent) {
	item := event.Item
	if p.last[item.branch] == event {
		return
	}
	p.last[item.branch] = event

	switch event.Kind {
	case mergeProgressRetargeting:
		p.log.Infof("%s: retargeting %v onto %s",
			event.Item.branch, event.Item.changeID, event.Base)
	case mergeProgressWaitingForForgeHead:
		p.log.Infof("%s: waiting for HEAD to update", event.Item.branch)
	case mergeProgressWaitingForMergeability:
		p.log.Infof("%s: waiting for merge readiness", event.Item.branch)
	case mergeProgressMerging:
		p.log.Infof("%s: merging %v: %s",
			event.Item.branch, event.Item.changeID, event.URL)
	case mergeProgressWaitingForMerge:
		p.log.Debugf("%s: waiting for merge", event.Item.branch)
	case mergeProgressSkipped:
		p.log.Infof("%s: skipped", event.Item.branch)
	}
}

// widgetMergeProgress reports merge progress through a Bubble Tea model.
type widgetMergeProgress struct {
	runner ui.ModelView
	theme  ui.Theme

	events chan tea.Msg
	// err is written before stopped is closed
	// and read only after receiving from stopped.
	// Closing stopped synchronizes the write with the read.
	err error

	stopped chan struct{}
	cancel  context.CancelFunc
}

const (
	// The merge progress widget needs enough room for the bar,
	// the summary row,
	// and one or two status lines without consuming the terminal.
	mergeProgressScrollRegionMinHeight = 4
	mergeProgressScrollRegionMaxHeight = 8
)

func newWidgetMergeProgress(
	runner ui.ModelView,
	theme ui.Theme,
) *widgetMergeProgress {
	return &widgetMergeProgress{
		runner: runner,
		theme:  theme,
	}
}

// Start launches the Bubble Tea progress model
// and returns a context canceled when the user cancels the widget.
//
// The caller must call Finish after the merge loop exits
// so the model goroutine can stop and report renderer errors.
func (p *widgetMergeProgress) Start(
	ctx context.Context,
	items []*mergeItem,
) context.Context {
	ctx, p.cancel = context.WithCancel(ctx)
	p.events = make(chan tea.Msg, 1)
	p.stopped = make(chan struct{})

	progressItems := make([]mergeprogress.Item, len(items))
	for idx, item := range items {
		progressItems[idx] = mergeprogress.Item{
			ID:    item.branch,
			State: mergeprogress.StatePending,
		}
	}

	model := &mergeProgressModel{
		Widget: mergeprogress.New(progressItems...).
			WithTheme(p.theme),
		events: p.events,
	}
	go func() {
		p.err = p.runner.RunModel(model, &ui.RunOptions{
			ScrollRegionMinHeight: mergeProgressScrollRegionMinHeight,
			ScrollRegionMaxHeight: mergeProgressScrollRegionMaxHeight,
		})
		if errors.Is(model.Err(), mergeprogress.ErrCanceled) {
			p.cancel()
		}
		close(p.stopped)
	}()
	return ctx
}

func (p *widgetMergeProgress) Event(event mergeProgressEvent) {
	if p.events == nil {
		return
	}

	select {
	case p.events <- widgetProgressEvent(event):
	case <-p.stopped:
	}
}

func (p *widgetMergeProgress) Finish() error {
	if p.events == nil {
		return nil
	}

	select {
	case p.events <- mergeProgressDone{}:
	case <-p.stopped:
	}
	<-p.stopped
	if p.err != nil {
		return fmt.Errorf("run merge progress: %w", p.err)
	}
	return nil
}

type mergeProgressModel struct {
	*mergeprogress.Widget

	events <-chan tea.Msg
}

func (m *mergeProgressModel) Init() tea.Cmd {
	return tea.Batch(m.Widget.Init(), waitMergeProgressEvent(m.events))
}

func (m *mergeProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(mergeProgressDone); ok {
		return m, tea.Quit
	}

	model, cmd := m.Widget.Update(msg)
	if widget, ok := model.(*mergeprogress.Widget); ok {
		m.Widget = widget
	}
	return m, tea.Batch(cmd, waitMergeProgressEvent(m.events))
}

// mergeProgressDone tells the Bubble Tea model
// that the merge loop has stopped publishing events.
type mergeProgressDone struct{}

// waitMergeProgressEvent blocks the Bubble Tea update loop
// until the merge loop publishes the next progress message.
func waitMergeProgressEvent(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-events
	}
}

func widgetProgressEvent(event mergeProgressEvent) mergeprogress.Event {
	item := event.Item
	switch event.Kind {
	case mergeProgressPreparing:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateActive,
			Message: fmt.Sprintf("%s: preparing %v", item.branch, item.changeID),
		}
	case mergeProgressPrepareFailed:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateFailed,
			Message: item.branch + ": prepare failed",
		}
	case mergeProgressRetargeting:
		return mergeprogress.Event{
			ItemID: item.branch,
			State:  mergeprogress.StateActive,
			Message: fmt.Sprintf("%s: retargeting %v onto %s",
				item.branch, item.changeID, event.Base),
		}
	case mergeProgressRetargetFailed:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateFailed,
			Message: item.branch + ": retarget failed",
		}
	case mergeProgressWaitingForForgeHead:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateActive,
			Message: item.branch + ": waiting for HEAD to update",
		}
	case mergeProgressForgeHeadFailed:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateFailed,
			Message: item.branch + ": HEAD did not update",
		}
	case mergeProgressWaitingForMergeability:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateActive,
			Message: item.branch + ": waiting for merge readiness",
		}
	case mergeProgressMergeabilityReady:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateActive,
			Message: item.branch + ": ready to merge",
		}
	case mergeProgressMergeabilityFailed:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateFailed,
			Message: item.branch + ": merge readiness failed",
		}
	case mergeProgressMerging:
		return mergeprogress.Event{
			ItemID: item.branch,
			State:  mergeprogress.StateActive,
			Message: fmt.Sprintf("%s: merging %v: %s",
				item.branch, item.changeID, event.URL),
		}
	case mergeProgressMergeFailed:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateFailed,
			Message: item.branch + ": merge failed",
		}
	case mergeProgressMergeRequested:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateMerged,
			Message: item.branch + ": merge requested",
		}
	case mergeProgressWaitingForMerge:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateActive,
			Message: item.branch + ": waiting for merge",
		}
	case mergeProgressMergeIncomplete:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateFailed,
			Message: item.branch + ": merge did not complete",
		}
	case mergeProgressMerged:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateMerged,
			Message: item.branch + ": merged",
		}
	case mergeProgressSyncFailed:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateFailed,
			Message: item.branch + ": sync failed",
		}
	case mergeProgressFailed:
		return mergeprogress.Event{
			ItemID: item.branch,
			State:  mergeprogress.StateFailed,
		}
	case mergeProgressSkipped:
		return mergeprogress.Event{
			ItemID:  item.branch,
			State:   mergeprogress.StateSkipped,
			Message: item.branch + ": skipped",
		}
	default:
		panic(fmt.Sprintf("unknown merge progress event kind: %d",
			event.Kind))
	}
}

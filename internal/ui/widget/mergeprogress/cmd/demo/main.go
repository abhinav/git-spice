// Command demo previews the merge progress widget.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget/mergeprogress"
)

func main() {
	var req request
	flag.IntVar(&req.items, "items", 12, "number of items")
	flag.IntVar(&req.merged, "merged", 0, "initial merged item count")
	flag.IntVar(&req.active, "active", 1, "initial active item count")
	flag.IntVar(&req.failed, "failed", 0, "initial failed item count")
	flag.IntVar(&req.skipped, "skipped", 0, "initial skipped item count")
	flag.StringVar(&req.message, "message",
		"feat1: waiting for CI checks",
		"detail message")
	flag.BoolVar(&req.animate, "animate", false, "animate progress")
	flag.BoolVar(&req.logs, "logs", false,
		"emit synthetic logs while the widget is active")
	flag.DurationVar(&req.tick, "tick", 750*time.Millisecond, "animation tick")
	flag.IntVar(&req.width, "width", 0, "initial widget width")
	flag.IntVar(&req.height, "height", 0, "initial terminal height")
	flag.BoolVar(&req.dark, "dark", true, "use the dark theme")
	flag.Parse()

	if err := req.run(ui.NewOutputWriter(os.Stdout)); err != nil {
		fmt.Fprintf(os.Stderr, "mergeprogress demo: %v\n", err)
		os.Exit(1)
	}
}

// request is the flag-decoded demo configuration.
type request struct {
	items   int
	merged  int
	active  int
	failed  int
	skipped int

	message string
	animate bool
	logs    bool
	tick    time.Duration
	width   int
	height  int
	dark    bool
}

func (r *request) run(output *ui.OutputWriter) error {
	widget := mergeprogress.New(r.progressItems()...).
		WithTheme(r.theme())
	if r.message != "" {
		_, _ = widget.Update(mergeprogress.Event{
			Message: r.message,
		})
	}

	var model tea.Model = widget
	if r.animate {
		model = &demoModel{
			Widget: widget,
			states: r.states(),
			tick:   r.tick,
			log: silog.New(&colorprofile.Writer{
				Forward: output,
				Profile: colorprofile.Detect(output.Unwrap(), os.Environ()),
			}, &silog.Options{
				Level: silog.LevelDebug,
			}),
			logs: r.logs,
		}
	}

	if err := ui.RunModel(model, &ui.RunOptions{
		Input:  os.Stdin,
		Output: output,
		Width:  r.width,
		Height: r.height,
	}); err != nil {
		return fmt.Errorf("run program: %w", err)
	}
	return nil
}

func (r *request) theme() ui.Theme {
	if r.dark {
		return ui.DefaultThemeDark()
	}
	return ui.DefaultThemeLight()
}

func (r *request) progressItems() []mergeprogress.Item {
	states := r.states()
	items := make([]mergeprogress.Item, len(states))
	for idx, state := range states {
		items[idx] = mergeprogress.Item{
			ID:    itemID(idx),
			State: state,
		}
	}
	return items
}

func (r *request) states() []mergeprogress.State {
	states := make([]mergeprogress.State, r.items)
	idx := 0
	for range r.merged {
		states[idx] = mergeprogress.StateMerged
		idx++
	}
	for range r.active {
		states[idx] = mergeprogress.StateActive
		idx++
	}
	for range r.failed {
		states[idx] = mergeprogress.StateFailed
		idx++
	}
	for range r.skipped {
		states[idx] = mergeprogress.StateSkipped
		idx++
	}
	return states
}

// demoModel adds synthetic timed progress to the real widget.
type demoModel struct {
	*mergeprogress.Widget

	states []mergeprogress.State
	tick   time.Duration
	log    *silog.Logger
	logs   bool
}

type tickMsg struct{}

func (m *demoModel) Init() tea.Cmd {
	return tea.Batch(m.Widget.Init(), m.nextTick())
}

func (m *demoModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tickMsg); ok {
		return m, m.advance()
	}

	model, cmd := m.Widget.Update(msg)
	if widget, ok := model.(*mergeprogress.Widget); ok {
		m.Widget = widget
	}
	return m, cmd
}

func (m *demoModel) advance() tea.Cmd {
	for idx, state := range m.states {
		if state == mergeprogress.StateActive {
			m.states[idx] = mergeprogress.StateMerged
			itemID := itemID(idx)
			_, cmd := m.Widget.Update(mergeprogress.Event{
				ItemID:  itemID,
				State:   mergeprogress.StateMerged,
				Message: itemID + ": merged",
			})
			return tea.Batch(
				cmd,
				m.logf("%s: pulled 1 new commit(s)", itemID),
				m.logf("%s: deleted (was %s)", itemID, fakeHash(idx)),
				m.nextTick(),
			)
		}
	}

	for idx, state := range m.states {
		if state == mergeprogress.StatePending {
			m.states[idx] = mergeprogress.StateActive
			itemID := itemID(idx)
			_, cmd := m.Widget.Update(mergeprogress.Event{
				ItemID:  itemID,
				State:   mergeprogress.StateActive,
				Message: itemID + ": waiting for CI checks",
			})
			return tea.Batch(
				cmd,
				m.logf("%s: retargeting #%d onto main...",
					itemID, changeNumber(idx)),
				m.logf("%s: updated #%d: https://example.test/pull/%d",
					itemID, changeNumber(idx), changeNumber(idx)),
				m.nextTick(),
			)
		}
	}

	return tea.Quit
}

func (m *demoModel) nextTick() tea.Cmd {
	return tea.Tick(m.tick, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (m *demoModel) logf(format string, args ...any) tea.Cmd {
	return func() tea.Msg {
		if m.logs {
			m.log.Infof(format, args...)
		}
		return nil
	}
}

func itemID(idx int) string {
	return fmt.Sprintf("feat%d", idx+1)
}

func changeNumber(idx int) int {
	return 1200 + idx + 1
}

func fakeHash(idx int) string {
	return fmt.Sprintf("%06x", 0xabc000+idx)
}

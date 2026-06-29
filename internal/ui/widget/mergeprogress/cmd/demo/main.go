// Command demo previews the merge progress widget.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"go.abhg.dev/gs/internal/mergequeue"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/branchtree"
	"go.abhg.dev/gs/internal/ui/widget/mergeprogress"
)

func main() {
	var req request
	flag.IntVar(&req.items, "items", 12, "number of items")
	flag.BoolVar(&req.noAnimate, "no-animate", false, "disable animation")
	flag.BoolVar(&req.noLogs, "no-logs", false,
		"disable synthetic logs while the widget is active")
	flag.BoolVar(&req.printTopology, "topology", false,
		"print the generated branch topology before running")
	flag.IntVar(&req.width, "width", 0, "initial widget width")
	flag.IntVar(&req.height, "height", 0, "initial terminal height")
	flag.BoolVar(&req.dark, "dark", true, "use the dark theme")
	flag.Int64Var(&req.seed, "seed", 1, "random seed")
	flag.Float64Var(&req.failRate, "fail-rate", 0.20,
		"probability that an item fails during prepare or run")
	flag.DurationVar(&req.minDelay, "min-delay", 500*time.Millisecond,
		"minimum synthetic item delay")
	flag.DurationVar(&req.maxDelay, "max-delay", 2*time.Second,
		"maximum synthetic item delay")
	flag.Parse()

	if err := req.run(ui.NewOutputWriter(os.Stdout)); err != nil {
		fmt.Fprintf(os.Stderr, "mergeprogress demo: %v\n", err)
		os.Exit(1)
	}
}

// request is the flag-decoded demo configuration.
type request struct {
	items int

	noAnimate     bool
	noLogs        bool
	printTopology bool
	width         int
	height        int
	dark          bool

	seed     int64
	failRate float64
	minDelay time.Duration
	maxDelay time.Duration
}

func (r *request) run(output *ui.OutputWriter) error {
	if err := r.validate(); err != nil {
		return err
	}

	log := silog.Nop()
	if !r.noLogs {
		log = silog.New(&colorprofile.Writer{
			Forward: output,
			Profile: colorprofile.Detect(output.Unwrap(), os.Environ()),
		}, &silog.Options{
			Level: silog.LevelDebug,
		})
	}

	scenario := r.scenario(log)
	if r.printTopology {
		fmt.Fprintln(output, "Generated topology:")
		if err := branchtree.Write(output, scenario.branchTree(),
			&branchtree.GraphOptions{Theme: r.theme()}); err != nil {
			return fmt.Errorf("render topology: %w", err)
		}
		fmt.Fprintln(output)
	}

	model := newDemoModel(mergeprogress.New(scenario.progressItems()...).
		WithTheme(r.theme()).
		WithAnimation(!r.noAnimate), scenario.items, log)

	if err := ui.RunModel(model, &ui.RunOptions{
		Input:  os.Stdin,
		Output: output,
		Width:  r.width,
		Height: r.height,
	}); err != nil {
		return fmt.Errorf("run program: %w", err)
	}
	if model.err != nil {
		log.Errorf("merge demo failed: %v", model.err)
	}
	return nil
}

func (r *request) validate() error {
	if r.items < 0 {
		return errors.New("items must be non-negative")
	}
	if r.failRate < 0 || r.failRate > 1 {
		return errors.New("fail-rate must be between 0 and 1")
	}
	if r.minDelay < 0 {
		return errors.New("min-delay must be non-negative")
	}
	if r.maxDelay < 0 {
		return errors.New("max-delay must be non-negative")
	}
	if r.minDelay > r.maxDelay {
		return errors.New("min-delay must be <= max-delay")
	}
	return nil
}

func (r *request) theme() ui.Theme {
	if r.dark {
		return ui.DefaultThemeDark()
	}
	return ui.DefaultThemeLight()
}

func (r *request) scenario(log *silog.Logger) *scenario {
	rng := rand.New(rand.NewSource(r.seed))
	items := make([]*syntheticItem, r.items)
	for idx := range items {
		var parent string
		if idx > 0 && rng.Intn(4) != 0 {
			parent = itemID(rng.Intn(idx))
		}

		fail := rng.Float64() < r.failRate
		failStage := failStageNone
		if fail {
			failStage = failStagePrepare
			if rng.Intn(2) == 0 {
				failStage = failStageRun
			}
		}

		items[idx] = &syntheticItem{
			id:           itemID(idx),
			parent:       parent,
			changeNumber: changeNumber(idx),
			prepareDelay: randomDelay(rng, r.minDelay, r.maxDelay),
			runDelay:     randomDelay(rng, r.minDelay, r.maxDelay),
			failStage:    failStage,
			log:          log,
		}
	}
	return &scenario{items: items}
}

func randomDelay(
	rng *rand.Rand,
	minDelay time.Duration,
	maxDelay time.Duration,
) time.Duration {
	if minDelay == maxDelay {
		return minDelay
	}
	return minDelay + time.Duration(rng.Int63n(int64(maxDelay-minDelay)+1))
}

type scenario struct {
	items []*syntheticItem
}

func (s *scenario) progressItems() []mergeprogress.Item {
	items := make([]mergeprogress.Item, len(s.items))
	for idx, item := range s.items {
		items[idx] = mergeprogress.Item{
			ID:    item.id,
			State: mergeprogress.StatePending,
		}
	}
	return items
}

func (s *scenario) branchTree() branchtree.Graph {
	items := make([]*branchtree.Item, len(s.items)+1)
	items[0] = &branchtree.Item{Branch: "trunk"}

	indexByID := make(map[string]int, len(s.items))
	for idx, item := range s.items {
		treeIdx := idx + 1
		items[treeIdx] = &branchtree.Item{Branch: item.id}
		indexByID[item.id] = treeIdx
	}

	for idx, item := range s.items {
		treeIdx := idx + 1
		if item.parent == "" {
			items[0].Aboves = append(items[0].Aboves, treeIdx)
			continue
		}
		items[indexByID[item.parent]].Aboves = append(
			items[indexByID[item.parent]].Aboves,
			treeIdx,
		)
	}

	return branchtree.Graph{
		Items: items,
		Roots: []int{0},
	}
}

// demoModel runs synthetic mergequeue work beside the real widget.
type demoModel struct {
	*mergeprogress.Widget

	ctx    context.Context
	cancel context.CancelFunc
	items  []mergequeue.Item
	events chan tea.Msg
	log    *silog.Logger
	err    error
}

func newDemoModel(
	widget *mergeprogress.Widget,
	items []*syntheticItem,
	log *silog.Logger,
) *demoModel {
	queueItems := make([]mergequeue.Item, len(items))
	for idx, item := range items {
		queueItems[idx] = item
	}

	ctx, cancel := context.WithCancel(context.Background())
	model := &demoModel{
		Widget: widget,
		ctx:    ctx,
		cancel: cancel,
		items:  queueItems,
		events: make(chan tea.Msg),
		log:    log,
	}
	for _, item := range items {
		item.events = model.events
	}
	return model
}

func (m *demoModel) Init() tea.Cmd {
	return tea.Batch(
		m.Widget.Init(),
		m.runScheduler(),
		m.waitEvent(),
	)
}

func (m *demoModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case schedulerDoneMsg:
		m.cancel()
		if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
			m.err = msg.err
		}
		return m, tea.Quit
	case mergeprogress.Event:
		model, cmd := m.Widget.Update(msg)
		if widget, ok := model.(*mergeprogress.Widget); ok {
			m.Widget = widget
		}
		return m, tea.Batch(cmd, m.waitEvent())
	}

	model, cmd := m.Widget.Update(msg)
	if widget, ok := model.(*mergeprogress.Widget); ok {
		m.Widget = widget
	}
	if errors.Is(m.Err(), mergeprogress.ErrCanceled) {
		m.cancel()
	}
	return m, cmd
}

func (m *demoModel) runScheduler() tea.Cmd {
	return func() tea.Msg {
		scheduler, err := mergequeue.New(m.items, &mergequeue.Options{
			Observer: demoObserver{
				ctx:    m.ctx,
				events: m.events,
				log:    m.log,
			},
		})
		if err == nil {
			err = scheduler.Run(m.ctx)
		}
		return schedulerDoneMsg{err: err}
	}
}

func (m *demoModel) waitEvent() tea.Cmd {
	return func() tea.Msg {
		select {
		case event := <-m.events:
			return event
		case <-m.ctx.Done():
			return schedulerDoneMsg{err: m.ctx.Err()}
		}
	}
}

type schedulerDoneMsg struct {
	err error
}

type demoObserver struct {
	ctx    context.Context
	events chan<- tea.Msg
	log    *silog.Logger
}

func (o demoObserver) Prepared(mergequeue.Item) {}

func (o demoObserver) Done(mergequeue.Item) {}

func (o demoObserver) Failed(item mergequeue.Item, _ error) {
	o.log.Errorf("%s: failed", item.ID())
	o.send(mergeprogress.Event{
		ItemID: item.ID(),
		State:  mergeprogress.StateFailed,
	})
}

func (o demoObserver) Skipped(item mergequeue.Item, _ mergequeue.SkipReason) {
	o.log.Warnf("%s: skipped", item.ID())
	o.send(mergeprogress.Event{
		ItemID: item.ID(),
		State:  mergeprogress.StateSkipped,
	})
}

func (o demoObserver) send(msg tea.Msg) {
	select {
	case o.events <- msg:
	case <-o.ctx.Done():
	}
}

type failStage uint8

const (
	failStageNone failStage = iota
	failStagePrepare
	failStageRun
)

var _ mergequeue.Item = (*syntheticItem)(nil)

type syntheticItem struct {
	id     string
	parent string

	changeNumber int
	prepareDelay time.Duration
	runDelay     time.Duration
	failStage    failStage

	events chan<- tea.Msg
	log    *silog.Logger
}

func (i *syntheticItem) ID() string {
	return i.id
}

func (i *syntheticItem) Parent() string {
	return i.parent
}

func (i *syntheticItem) Prepare(ctx context.Context) error {
	i.emit(ctx, mergeprogress.Event{
		ItemID: i.id,
		State:  mergeprogress.StateActive,
	})
	if err := sleep(ctx, i.prepareDelay); err != nil {
		return err
	}
	if i.parent != "" {
		i.log.Infof("%s: retargeting #%d onto main...",
			i.id, i.changeNumber)
		i.log.Infof("%s: updated #%d: https://example.test/pull/%d",
			i.id, i.changeNumber, i.changeNumber)
	}
	if i.failStage == failStagePrepare {
		i.emit(ctx, mergeprogress.Event{
			ItemID: i.id,
			State:  mergeprogress.StateFailed,
		})
		return errors.New("prepare failed")
	}
	return nil
}

func (i *syntheticItem) Run(ctx context.Context) error {
	i.emit(ctx, mergeprogress.Event{
		ItemID: i.id,
		State:  mergeprogress.StateActive,
	})
	if err := sleep(ctx, i.runDelay); err != nil {
		return err
	}
	if i.failStage == failStageRun {
		i.emit(ctx, mergeprogress.Event{
			ItemID: i.id,
			State:  mergeprogress.StateFailed,
		})
		return errors.New("merge readiness failed")
	}

	i.log.Infof("%s: pulled 1 new commit(s)", i.id)
	i.log.Infof("%s: deleted (was %s)", i.id, fakeHash(i.changeNumber))
	i.emit(ctx, mergeprogress.Event{
		ItemID: i.id,
		State:  mergeprogress.StateMerged,
	})
	return nil
}

func (i *syntheticItem) emit(ctx context.Context, event mergeprogress.Event) {
	select {
	case i.events <- event:
	case <-ctx.Done():
	}
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func itemID(idx int) string {
	return fmt.Sprintf("feat%d", idx+1)
}

func changeNumber(idx int) int {
	return 1200 + idx + 1
}

func fakeHash(n int) string {
	return fmt.Sprintf("%06x", 0xabc000+n)
}

package uitest

import (
	"cmp"
	"io"
	"iter"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/vito/midterm"
	"go.abhg.dev/gs/internal/ui"
)

// Driver drives a ui.InteractiveView from outside
// by sending it messages (e.g. simulating key presses).
//
// The interactive view is run in a coroutine,
// allowing the entire operation to be single-threaded.
// (Bubble Tea's internal state is not thread-safe.)
type Driver struct {
	t    testing.TB
	w, h int

	// messages enqueued for the interactive view
	// from outside the coroutine.
	inbound []tea.Msg

	// formView returns the current view of the active form.
	// If it's nil, there's no active form.
	formView func() string

	// Advances the coroutine to the next yield point.
	step func()
}

// DriverOptions are options for creating a [Driver].
type DriverOptions struct {
	// Width and Height specify the initial window size.
	//
	// Defaults to 80x24.
	Width, Height int
}

// Drive runs the given function with a [ui.InteractiveView] in a coroutine
// (think single-threaded).
//
// The function may use the interactive view as long as the driver is active.
func Drive(
	t testing.TB,
	do func(ui.InteractiveView),
	opts *DriverOptions,
) *Driver {
	opts = cmp.Or(opts, &DriverOptions{})
	d := &Driver{
		t: t,
		w: cmp.Or(opts.Width, 80),
		h: cmp.Or(opts.Height, 24),
	}

	// Call the user-provided 'Do' function in a coroutine
	// (note: not a goroutine; we're abusing iter.Pull for this).
	//
	// This function will run in a blocking fashion
	// until it has processed all events on its own.
	// It will yield control back to us when it needs more inputs,
	// at which point the user can call Driver.Whatever()
	// to post new events and advance the state.
	next, stop := iter.Pull(func(yield func(struct{}) bool) {
		// sentinel value to indicate that the function stopped early
		// intentionally due to a call to stop().
		//
		// We'll use this in the panic handler to decide
		// whether we need to re-panic or not.
		var exit any = new(int)
		defer func() {
			if pval := recover(); pval != nil && pval != exit {
				panic(pval)
			}
		}()

		do(&driverView{
			w: d.w,
			h: d.h,
			yield: func() {
				// The coroutine should never be exited early;
				// that means that the form wasn't run fully.
				//
				// do should always exit cleanly,
				// which means stop() will not be called.
				if !yield(struct{}{}) {
					t.Errorf("view coroutine exited early")
					panic(exit)
				}
			},
			driver: d,
		})
	})
	d.step = func() { next() }
	t.Cleanup(stop) // early return = break
	return d
}

// PressN simulates pressing the given key n times.
// It blocks until the model has processed all resulting messages.
func (d *Driver) PressN(key tea.KeyType, n int) {
	d.t.Helper()

	for range n {
		d.inbound = append(d.inbound, tea.KeyMsg{Type: key})
	}
	d.step()
}

// Press simulates pressing the given key once.
// It blocks until the model has processed all resulting messages.
func (d *Driver) Press(key tea.KeyType) {
	d.t.Helper()

	d.PressN(key, 1)
}

// Type simulates typing the given string.
// It blocks until the model has processed all resulting messages.
func (d *Driver) Type(s string) {
	for _, r := range s {
		d.inbound = append(d.inbound, tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune{r},
		})
	}

	d.step()
}

// Snapshot returns a snapshot of the current view as a string,
// with no terminal control codes.
func (d *Driver) Snapshot() string {
	d.t.Helper()

	if d.formView == nil {
		d.t.Fatalf("no active form")
	}

	// There doesn't appear to be a way to get the view
	// to render without coloring codes (functionality is internal),
	// so use midterm to render it to a virtual terminal
	// and extract just the text content.
	term := midterm.NewAutoResizingTerminal()
	_, _ = io.WriteString(term, d.formView())

	var lines []string
	for _, row := range term.Content {
		row = trimRightWS(row)
		lines = append(lines, string(row))
	}

	// Trim trailing empty lines.
	for i := len(lines) - 1; i >= 0; i-- {
		if len(lines[i]) > 0 {
			lines = lines[:i+1]
			break
		}
	}

	return strings.Join(lines, "\n") + "\n"
}

// driverView is a ui.InteractiveView
// that receives events from a [Driver].
type driverView struct {
	w, h  int
	yield func()

	driver *Driver
}

var _ ui.InteractiveView = (*driverView)(nil)

func (v *driverView) Prompt(fields ...ui.Field) error {
	form := ui.NewForm(fields...)

	// WindowSizeMsg must always be first
	// after initialization.
	var pending []tea.Msg
	if cmd := form.Init(); cmd != nil {
		pending = append(pending, cmd())
	}
	pending = append(pending, tea.WindowSizeMsg{
		Width:  v.w,
		Height: v.h,
	})

	v.driver.formView = form.View
	defer func() {
		v.driver.formView = nil
	}()

	// Also grab any inbound messages if any.
	pending = append(pending, v.driver.inbound...)
	v.driver.inbound = v.driver.inbound[:0]

	for {
		// As long as there's nothing pending,
		// yield control back to the Driver to get more input.
		for len(pending) == 0 {
			// NB: if the coroutine exited early,
			// this will panic and be caught by the panic handler
			// in the Drive function's definition.
			v.yield()

			pending = append(pending, v.driver.inbound...)
			v.driver.inbound = v.driver.inbound[:0]
		}

		var msg tea.Msg
		msg, pending = pending[0], pending[1:]
		switch msg := msg.(type) {
		case tea.QuitMsg:
			return nil

		case tea.BatchMsg:
			// Batch messages should be inserted to the front.
			var prepend []tea.Msg
			for _, cmd := range msg {
				if msg := cmd(); msg != nil {
					prepend = append(prepend, msg)
				}
			}
			pending = append(prepend, pending...)

		default:
			// For all other messages,
			// send them to the form for processing.
			// Form mutates its state internally,
			// so we don't need to capture the new state.
			if _, cmd := form.Update(msg); cmd != nil {
				if msg := cmd(); msg != nil {
					pending = append(pending, msg)
				}
			}
		}
	}
}

func (v *driverView) Write(p []byte) (n int, err error) {
	// Don't do anything for now.
	// If necessary, we can capture in the future.
	return len(p), nil
}

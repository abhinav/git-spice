package ui

import (
	"bytes"
	"io"
	"iter"
	"sync"

	tea "charm.land/bubbletea/v2"
	"go.abhg.dev/gs/internal/must"
)

// OutputWriter coordinates regular log output with Bubble Tea rendering.
//
// When no program is active, writes go directly to the underlying writer.
// When a program is active, lines are routed through the Bubble Tea program,
// so the renderer can place them without corrupting the rendered frame.
type OutputWriter struct {
	mu sync.Mutex
	// cond wakes sendLoop when an active program has complete lines to print.
	cond *sync.Cond
	wg   sync.WaitGroup

	out     io.Writer    // underlying writer
	program printProgram // bubble tea program
	closing bool         // sendLoop should exit instead of waiting for output

	// tea.Println needs complete lines
	// so we'll buffer them in memory and flush
	// as they're completed.
	lines lineBuffer
}

var (
	_ io.Writer = (*OutputWriter)(nil)
	_ io.Closer = (*OutputWriter)(nil)
)

// NewOutputWriter builds an OutputWriter over the given writer.
func NewOutputWriter(out io.Writer) *OutputWriter {
	w := &OutputWriter{out: out}
	w.cond = sync.NewCond(&w.mu)
	w.wg.Go(w.sendLoop)
	return w
}

type printProgram interface{ Send(tea.Msg) }

// printTo routes completed output lines through program until the returned
// cleanup function is called.
//
// Callers must call the cleanup function after the Bubble Tea program stops.
// The cleanup function writes any buffered partial line directly to the
// underlying writer because the program can no longer accept print messages.
func (w *OutputWriter) printTo(program printProgram) (cleanup func()) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.program = program
	w.lines = lineBuffer{}
	w.cond.Signal()

	return func() {
		w.mu.Lock()
		defer w.mu.Unlock()

		must.Bef(program == w.program, "OutputWriter cleanup called for wrong program")

		// Drain any unflushed output to the regular writer
		// now that the program has stopped.
		if tail := w.lines.Drain(); len(tail) > 0 {
			_, _ = w.out.Write(tail)
		}
		w.program = nil
		w.lines = lineBuffer{}
	}
}

func (w *OutputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closing || w.program == nil {
		return w.out.Write(p)
	}

	w.lines.Write(p)
	w.cond.Signal()
	return len(p), nil
}

// Close stops the background print dispatcher.
//
// Callers should stop the active program and run the printTo cleanup before
// Close.
// Close does not flush buffered active-program output.
func (w *OutputWriter) Close() error {
	w.mu.Lock()
	w.closing = true
	w.cond.Signal()
	w.mu.Unlock()

	w.wg.Wait()
	return nil
}

// sendLoop owns delivery of completed print lines to the active program.
// Write only appends to the buffer and signals this loop,
// so ordinary log output never waits on Bubble Tea's update loop.
func (w *OutputWriter) sendLoop() {
	for {
		w.mu.Lock()
		for !w.closing && (w.program == nil || !w.lines.HasLine()) {
			w.cond.Wait()
		}
		if w.closing {
			w.mu.Unlock()
			return
		}

		program := w.program
		var msgs []tea.Msg
		for line := range w.lines.TakeLines() {
			// Bubble Tea print messages add their own newline.
			// Trim the CR from CRLF output before routing the line.
			line = bytes.TrimSuffix(line, []byte("\r"))

			msgs = append(msgs, tea.Println(string(line))())
		}
		w.mu.Unlock()

		for _, msg := range msgs {
			// Use Send instead of Print or Printf
			// so late writes after shutdown are ignored
			// instead of blocking on a stopped program.
			program.Send(msg)
		}
	}
}

// Unwrap reports the underlying writer.
//
// Bubble Tea must render to the terminal stream directly.
// Otherwise, model frames would be fed back through OutputWriter as print
// messages.
func (w *OutputWriter) Unwrap() io.Writer {
	return w.out
}

// lineBuffer splits active program output into completed lines and a tail.
//
// Completed lines are safe to route through Bubble Tea while the program is
// running.
// The tail must remain available for direct output after the program exits.
type lineBuffer struct {
	pending []byte // unconsumed output, including the final partial line
}

// Write appends raw output bytes without interpreting partial lines.
func (b *lineBuffer) Write(p []byte) {
	b.pending = append(b.pending, p...)
}

// HasLine reports whether the buffer has at least one complete line.
func (b *lineBuffer) HasLine() bool {
	return bytes.Contains(b.pending, []byte{'\n'})
}

// TakeLines yields complete lines and consumes them from the buffer.
//
// TakeLines do not include the trailing newline byte.
// Any partial line remains buffered for the next write or Drain call.
func (b *lineBuffer) TakeLines() iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for {
			idx := bytes.IndexByte(b.pending, '\n')
			if idx < 0 {
				return
			}

			line := b.pending[:idx]
			b.pending = b.pending[idx+1:]
			if !yield(line) {
				return
			}
		}
	}
}

// Drain consumes and returns all remaining buffered output.
func (b *lineBuffer) Drain() []byte {
	tail := b.pending
	b.pending = nil
	return tail
}

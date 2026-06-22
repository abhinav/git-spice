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

	out     io.Writer    // underlying writer
	program printProgram // bubble tea program

	// tea.Println needs complete lines
	// so we'll buffer them in memory and flush
	// as they're completed.
	lines lineBuffer
}

var _ io.Writer = (*OutputWriter)(nil)

// NewOutputWriter builds an OutputWriter over the given writer.
func NewOutputWriter(out io.Writer) *OutputWriter {
	return &OutputWriter{out: out}
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

	if w.program == nil {
		return w.out.Write(p)
	}

	w.lines.Write(p)
	for line := range w.lines.TakeLines() {
		// Bubble Tea print messages add their own newline.
		// Trim the CR from CRLF output before routing the line.
		line = bytes.TrimSuffix(line, []byte("\r"))

		// Use Send instead of Print or Printf
		// so late writes after shutdown are ignored
		// instead of blocking on a stopped program.
		w.program.Send(tea.Println(string(line))())
	}
	return len(p), nil
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

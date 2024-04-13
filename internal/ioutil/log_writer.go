// Package ioutil provides I/O utilities.
package ioutil

import (
	"bytes"
	"io"
	"sync"

	"github.com/charmbracelet/log"
)

// LogWriter builds and returns an io.Writer that
// writes messages to the given logger.
// If the logger is nil, a no-op writer is returned.
//
// If prefix is non-empty, it is prepended to each message.
// The done function must be called when the writer is no longer needed.
// It will flush any buffered text to the logger.
//
// The returned writer is not thread-safe.
func LogWriter(log *log.Logger, lvl log.Level) (w io.Writer, done func()) {
	if log == nil {
		return io.Discard, func() {}
	}

	w, flush := newPrintfWriter(func(msg string, args ...any) {
		log.Logf(lvl, msg, args...)
	}, "")
	return w, flush
}

// TestOutput allows writing to the test log.
type TestOutput interface {
	Logf(format string, args ...any)
	Cleanup(f func())
}

// TestOutputWriter builds and returns an io.Writer that
// writes messages to the given testing.TB.
// The returned writer is not thread-safe.
func TestOutputWriter(t TestOutput, prefix string) (w io.Writer) {
	w, flush := newPrintfWriter(t.Logf, prefix)
	t.Cleanup(flush)
	return w
}

// printfWriter is an io.Writer that writes to a log.Logger.
type printfWriter struct {
	// printf implementation should add a newline at the end.
	printf func(string, ...any)
	prefix string
	buff   bytes.Buffer
	mu     sync.Mutex
}

var _ io.Writer = (*printfWriter)(nil)

func newPrintfWriter(printf func(string, ...any), prefix string) (io.Writer, func()) {
	w := &printfWriter{
		printf: printf,
		prefix: prefix,
	}
	return w, w.flush
}

var _newline = []byte{'\n'}

func (w *printfWriter) Write(bs []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	total := len(bs)
	for len(bs) > 0 {
		var (
			line []byte
			ok   bool
		)
		line, bs, ok = bytes.Cut(bs, _newline)
		if !ok {
			// No newline. Buffer and wait for more.
			w.buff.Write(line)
			break
		}

		if w.buff.Len() == 0 {
			// No prior partial write. Flush.
			w.printf("%s%s", w.prefix, line)
			continue
		}

		// Flush prior partial write.
		w.buff.Write(line)
		w.printf("%s%s", w.prefix, w.buff.Bytes())
		w.buff.Reset()
	}
	return total, nil
}

// flush flushes buffered text, even if it doesn't end with a newline.
func (w *printfWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.buff.Len() > 0 {
		w.printf("%s%s", w.prefix, w.buff.Bytes())
		w.buff.Reset()
	}
}

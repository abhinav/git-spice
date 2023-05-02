package logx

import (
	"bytes"
	"io"
	"log"
)

// Writer builds and returns an io.Writer that
// writes messages to the given logger.
//
// The returned writer is not thread-safe.
func Writer(log *log.Logger, prefix string) (w io.Writer, done func()) {
	lw := &logWriter{log: log, prefix: prefix}
	return lw, lw.flush
}

// logWriter is an io.Writer that writes to a log.Logger.
type logWriter struct {
	// Subset of the *log.Logger interface.
	log interface {
		Printf(string, ...interface{})
	}

	prefix string
	buff   bytes.Buffer
}

var _ io.Writer = (*logWriter)(nil)

var _newline = []byte{'\n'}

func (w *logWriter) Write(bs []byte) (int, error) {
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
			w.log.Printf("%s%s", w.prefix, line)
			continue
		}

		// Flush prior partial write.
		w.buff.Write(line)
		w.log.Printf("%s%s", w.prefix, w.buff.Bytes())
		w.buff.Reset()
	}
	return total, nil
}

// flush flushes buffered text, even if it doesn't end with a newline.
func (w *logWriter) flush() {
	if w.buff.Len() > 0 {
		w.log.Printf("%s%s", w.prefix, w.buff.Bytes())
		w.buff.Reset()
	}
}

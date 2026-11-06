package gitedit

import (
	"bufio"
	"bytes"
	"io"
	"iter"
)

// lineReader is an io.Reader that reads from a source,
// splits the input into lines,
// and stops when stopFn returns true for a line.
//
// stopFn receives each line without the trailing newline.
// If stopFn returns true, the reader stops and reports io.EOF;
// the triggering line is not included in the output.
// If stopFn always returns false, the output is identical to the input.
type lineReader struct {
	next   func() ([]byte, error, bool)
	stop   func()
	stopFn func([]byte) bool
	buf    []byte
}

var _ io.Reader = (*lineReader)(nil)

func newLineReader(r io.Reader, stopFn func([]byte) bool) *lineReader {
	next, stop := iter.Pull2(scanLines(r))
	return &lineReader{
		next:   next,
		stop:   stop,
		stopFn: stopFn,
	}
}

func (r *lineReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(r.buf) == 0 {
		line, err, ok := r.next()
		if !ok && err == nil {
			err = io.EOF
		}
		if err != nil {
			return 0, err
		}

		// stopFn expects line without trailing newline.
		if ln := bytes.TrimSuffix(line, []byte{'\n'}); r.stopFn(ln) {
			r.stop()
			return 0, io.EOF
		}

		r.buf = append(r.buf[:0], line...)
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// scanLines returns a push iterator over the lines of r,
// where each token includes the trailing newline if present.
func scanLines(r io.Reader) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		scanner := bufio.NewScanner(r)
		scanner.Split(scanLinesWithNL)
		for scanner.Scan() {
			if !yield(scanner.Bytes(), nil) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			yield(nil, err)
		}
	}
}

// scanLinesWithNL is a bufio.SplitFunc that splits on newlines,
// preserving the trailing '\n' in the token
// (as opposed to bufio.Scanner's default behavior of stripping it).
// Final lines are emitted as-is even if they don't end with a newline.
func scanLinesWithNL(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		// Found newline.
		// Advance past newline
		// and include it in the token.
		return i + 1, data[:i+1], nil
	}
	if atEOF && len(data) > 0 {
		// Input ends without a trailing newline.
		return len(data), data, nil
	}
	return 0, nil, nil // request more data
}

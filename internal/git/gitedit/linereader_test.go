package gitedit

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLineReader(t *testing.T) {
	// allowAll is a stop function that never stops.
	allowAll := func([]byte) bool { return false }

	tests := []struct {
		name string
		give string
		stop func([]byte) bool
		want string
	}{
		{
			name: "PassThrough",
			give: "hello\nworld\n",
			stop: allowAll,
			want: "hello\nworld\n",
		},
		{
			name: "EmptyInput",
			stop: allowAll,
		},
		{
			name: "NoTrailingNewline",
			give: "hello\nworld",
			stop: allowAll,
			want: "hello\nworld",
		},
		{
			name: "StopAtLine",
			give: "keep\nSTOP\ndiscard\n",
			stop: func(line []byte) bool {
				return string(line) == "STOP"
			},
			want: "keep\n",
		},
		{
			name: "StopAtFirstLine",
			give: "STOP\nignored\n",
			stop: func(line []byte) bool {
				return string(line) == "STOP"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newLineReader(strings.NewReader(tt.give), tt.stop)
			got, err := io.ReadAll(r)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestLineReader_smallReads(t *testing.T) {
	r := newLineReader(
		strings.NewReader("hello\nworld\n"),
		func([]byte) bool { return false },
	)

	// Read one byte at a time to exercise
	// the pending-output draining path.
	var result []byte
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}

	assert.Equal(t, "hello\nworld\n", string(result))
}

func TestLineReader_zeroLengthRead(t *testing.T) {
	src := &countingReader{
		data: []byte("hello\nworld\n"),
	}
	r := newLineReader(src, func([]byte) bool { return false })

	n, err := r.Read(make([]byte, 0))
	assert.Equal(t, 0, n)
	assert.NoError(t, err)
	assert.Equal(t, 0, src.reads)

	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "hello\nworld\n", string(got))
}

func TestLineReader_emptyLines(t *testing.T) {
	r := newLineReader(
		strings.NewReader("a\n\nb\n\n"),
		func([]byte) bool { return false },
	)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "a\n\nb\n\n", string(got))
}

func TestLineReader_stopAtFinalUnterminatedLine(t *testing.T) {
	r := newLineReader(
		strings.NewReader("keep\nSTOP"),
		func(line []byte) bool {
			return string(line) == "STOP"
		},
	)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "keep\n", string(got))
}

func TestLineReader_lineSplitAcrossSourceReads(t *testing.T) {
	r := newLineReader(
		&chunkReader{
			chunks: [][]byte{
				[]byte("hel"),
				[]byte("lo\nwo"),
				[]byte("rld"),
				[]byte("\n"),
			},
		},
		func([]byte) bool { return false },
	)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "hello\nworld\n", string(got))
}

func TestLineReader_longLineOverInternalBuffer(t *testing.T) {
	long := strings.Repeat("a", 10000)
	in := long + "\n" + "tail"
	r := newLineReader(
		strings.NewReader(in),
		func([]byte) bool { return false },
	)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, in, string(got))
}

func FuzzLineReader_identity(f *testing.F) {
	f.Add([]byte("hello\nworld\n"))
	f.Add([]byte("no trailing\nnewline"))
	f.Fuzz(func(t *testing.T, give []byte) {
		r := newLineReader(
			bytes.NewReader(give),
			func([]byte) bool { return false },
		)
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if string(got) != string(give) {
			t.Errorf("want: %q\n got: %q", string(give), string(got))
		}
	})
}

type chunkReader struct {
	chunks [][]byte
	idx    int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.idx >= len(r.chunks) {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[r.idx])
	r.chunks[r.idx] = r.chunks[r.idx][n:]
	if len(r.chunks[r.idx]) == 0 {
		r.idx++
	}
	return n, nil
}

type countingReader struct {
	data  []byte
	pos   int
	reads int
}

func (r *countingReader) Read(p []byte) (int, error) {
	r.reads++
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	if r.pos >= len(r.data) {
		return n, io.EOF
	}
	return n, nil
}

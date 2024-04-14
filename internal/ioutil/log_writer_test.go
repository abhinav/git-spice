package ioutil

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestLogWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf)
	writer, done := LogWriter(logger, log.InfoLevel)

	_, err := fmt.Fprint(writer, "hello world")
	require.NoError(t, err)
	done()

	assert.Equal(t, "INFO hello world\n", buf.String())
}

func TestLogWriter_nil(t *testing.T) {
	writer, done := LogWriter(nil, log.InfoLevel)

	_, err := fmt.Fprint(writer, "hello world")
	require.NoError(t, err)
	done()
}

func TestTestOutputWriter(t *testing.T) {
	var out testOutputStub
	writer := TestOutputWriter(&out, "prefix: ")

	fmt.Fprint(writer, "hello world")
	out.cleanup()

	assert.Equal(t, []string{"prefix: hello world"}, out.logs)
}

type testOutputStub struct {
	logs    []string
	cleanup func()
}

func (t *testOutputStub) Logf(format string, args ...any) {
	t.logs = append(t.logs, fmt.Sprintf(format, args...))
}

func (t *testOutputStub) Cleanup(f func()) {
	old := t.cleanup
	t.cleanup = func() {
		f()
		if old != nil {
			old()
		}
	}
}

func TestPrintfWriter(t *testing.T) {
	tests := []struct {
		desc   string
		prefix string
		writes []string
		want   []string
	}{
		{desc: "empty"},
		{
			desc:   "single line",
			writes: []string{"hello world"},
			want:   []string{"hello world"},
		},
		{
			desc:   "single line/prefix",
			prefix: "prefix: ",
			writes: []string{"hello world"},
			want:   []string{"prefix: hello world"},
		},
		{
			desc:   "single line/newline",
			writes: []string{"hello world\n"},
			want:   []string{"hello world"},
		},
		{
			desc:   "single line/newline and prefix",
			prefix: "prefix: ",
			writes: []string{"hello world\n"},
			want:   []string{"prefix: hello world"},
		},
		{
			desc:   "multi line",
			writes: []string{"foo\n", "bar\n"},
			want:   []string{"foo", "bar"},
		},
		{
			desc:   "newline with separate write",
			writes: []string{"foo", "\n", "bar\n"},
			want:   []string{"foo", "bar"},
		},
		{
			desc:   "line across many writes",
			writes: []string{"f", "oo\nb", "ar\nb", "az\n"},
			want:   []string{"foo", "bar", "baz"},
		},
		{
			desc: "empty line",
			writes: []string{
				"foo\n",
				"\n",
				"bar\n",
			},
			want: []string{"foo", "", "bar"},
		},
		{
			desc:   "empty line/prefix",
			prefix: "prefix: ",
			writes: []string{
				"foo\n",
				"\n",
				"bar\n",
			},
			want: []string{"prefix: foo", "prefix: ", "prefix: bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			var got []string
			w, flush := LogfWriter(
				func(format string, args ...any) {
					got = append(got, fmt.Sprintf(format, args...))
				},
				tt.prefix,
			)

			for _, s := range tt.writes {
				fmt.Fprint(w, s)
			}
			flush()

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPrintfWriterRapid(t *testing.T) {
	rapid.Check(t, testPrintfWriterRapid)
}

func FuzzPrintfWriterRapid(f *testing.F) {
	f.Fuzz(rapid.MakeFuzz(testPrintfWriterRapid))
}

func testPrintfWriterRapid(t *rapid.T) {
	var gotBuff bytes.Buffer
	w, flush := LogfWriter(func(format string, args ...any) {
		_, err := fmt.Fprintf(&gotBuff, format+"\n", args...)
		assert.NoError(t, err)
	}, "")

	var wantBuff bytes.Buffer
	chunks := rapid.SliceOf(rapid.SliceOf(rapid.Byte())).Draw(t, "chunks")
	for _, chunk := range chunks {
		wantBuff.Write(chunk)
		_, err := w.Write(chunk)
		require.NoError(t, err)
	}

	flush()

	got := strings.TrimSuffix(gotBuff.String(), "\n")
	want := strings.TrimSuffix(wantBuff.String(), "\n")

	assert.Equal(t, want, got)
}

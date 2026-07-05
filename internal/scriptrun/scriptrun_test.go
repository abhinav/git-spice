package scriptrun

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog"
)

func TestRunner_Run_shellScript(t *testing.T) {
	r := &Runner{Log: silog.Nop()}

	res, err := r.Run(t.Context(), &RunRequest{
		Script: `echo "hello"; echo "world" >&2; exit 0`,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	assert.Equal(t, "hello\n", string(res.Stdout))
	assert.Equal(t, "world\n", string(res.Stderr))
}

func TestRunner_Run_envVars(t *testing.T) {
	r := &Runner{Log: silog.Nop()}

	res, err := r.Run(t.Context(), &RunRequest{
		Script: `echo "$FOO:$BAR"`,
		Env:    []string{"FOO=foo-val", "BAR=bar-val"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	assert.Equal(t, "foo-val:bar-val\n", string(res.Stdout))
}

func TestRunner_Run_positionalArgs(t *testing.T) {
	r := &Runner{
		Log: silog.Nop(),
	}

	res, err := r.Run(t.Context(), &RunRequest{
		Script: `echo "$1-$2"`,
		Args:   []string{"alpha", "beta"},
	})
	require.NoError(t, err)
	assert.Equal(t, "alpha-beta\n", string(res.Stdout))
}

func TestRunner_Run_workingDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t,
		os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("here"), 0o600))

	r := &Runner{Log: silog.Nop()}
	res, err := r.Run(t.Context(), &RunRequest{
		Script: `cat marker.txt`,
		Dir:    dir,
	})
	require.NoError(t, err)
	assert.Equal(t, "here", string(res.Stdout))
}

func TestRunner_Run_stdin(t *testing.T) {
	r := &Runner{Log: silog.Nop()}

	res, err := r.Run(t.Context(), &RunRequest{
		Script: `cat`,
		Stdin:  strings.NewReader("piped input"),
	})
	require.NoError(t, err)
	assert.Equal(t, "piped input", string(res.Stdout))
}

func TestRunner_Run_nonZeroExit(t *testing.T) {
	r := &Runner{Log: silog.Nop()}

	res, err := r.Run(t.Context(), &RunRequest{
		Script: `echo "out"; echo "err" >&2; exit 7`,
	})
	require.NoError(t, err, "non-zero exit must not be an error")
	assert.Equal(t, 7, res.ExitCode)
	assert.Equal(t, "out\n", string(res.Stdout))
	assert.Equal(t, "err\n", string(res.Stderr))
}

func TestRunner_Run_streamsOutputBeforeExit(t *testing.T) {
	r := &Runner{Log: silog.Nop()}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	stdout := newSignalWriter()
	stderr := newSignalWriter()
	done := make(chan error, 1)
	go func() {
		_, err := r.Run(ctx, &RunRequest{
			Script: `echo stdout; echo stderr >&2; sleep 2`,
			Stdout: stdout,
			Stderr: stderr,
		})
		done <- err
	}()

	requireWrittenBeforeExit(t, stdout.Written(), done)
	requireWrittenBeforeExit(t, stderr.Written(), done)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("script did not exit after context cancellation")
	}
}

func TestRunner_Run_streamsOutputWithoutCapture(t *testing.T) {
	r := &Runner{Log: silog.Nop()}

	var stdout, stderr bytes.Buffer
	res, err := r.Run(t.Context(), &RunRequest{
		Script: `echo stdout; echo stderr >&2`,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	assert.Equal(t, "stdout\n", stdout.String())
	assert.Equal(t, "stderr\n", stderr.String())
	assert.Empty(t, res.Stdout)
	assert.Empty(t, res.Stderr)
}

func TestRunner_Run_shebangScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shebang scripts are POSIX-only")
	}
	r := &Runner{Log: silog.Nop()}

	res, err := r.Run(t.Context(), &RunRequest{
		Script: "#!/bin/sh\necho \"shebang-output\"\nexit 0\n",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	assert.Equal(t, "shebang-output\n", string(res.Stdout))
}

func TestRunner_Run_shebangReceivesArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shebang scripts are POSIX-only")
	}
	r := &Runner{
		Log: silog.Nop(),
	}

	res, err := r.Run(t.Context(), &RunRequest{
		Script: "#!/bin/sh\necho \"$1=$2\"\n",
		Args:   []string{"first", "second"},
	})
	require.NoError(t, err)
	assert.Equal(t, "first=second\n", string(res.Stdout))
}

func TestRunner_Run_scriptFilePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script files are POSIX-only")
	}
	path := filepath.Join(t.TempDir(), "script with space.sh")
	require.NoError(t, os.WriteFile(
		path,
		[]byte("#!/bin/sh\necho \"$1:$2\"\n"),
		0o700,
	))

	r := &Runner{
		Log: silog.Nop(),
	}
	res, err := r.Run(t.Context(), &RunRequest{
		Script: path,
		Args:   []string{"one", "two"},
	})
	require.NoError(t, err)
	assert.Equal(t, "one:two\n", string(res.Stdout))
}

func TestRunner_Run_emptyScript(t *testing.T) {
	r := &Runner{Log: silog.Nop()}

	_, err := r.Run(t.Context(), &RunRequest{
		Script: "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty script")
}

func TestRunner_Run_nilRequest(t *testing.T) {
	r := &Runner{Log: silog.Nop()}

	assert.Panics(t, func() {
		_, _ = r.Run(t.Context(), nil)
	})
}

func TestRunner_Run_contextCancellation(t *testing.T) {
	r := &Runner{Log: silog.Nop()}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := r.Run(ctx, &RunRequest{
		Script: `sleep 30`,
	})
	require.Error(t, err)
	// Cancelled or killed by context; either is acceptable.
	assert.True(t, errors.Is(err, context.Canceled) ||
		strings.Contains(err.Error(), "signal:") ||
		strings.Contains(err.Error(), "killed") ||
		strings.Contains(err.Error(), "context canceled"),
		"unexpected error: %v", err)
}

func TestRunner_Run_contextCancelsRunningScript(t *testing.T) {
	r := &Runner{Log: silog.Nop()}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := r.Run(ctx, &RunRequest{
		Script: `sleep 1`,
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRunner_Run_nilLogger(t *testing.T) {
	r := &Runner{} // no logger

	res, err := r.Run(t.Context(), &RunRequest{
		Script: `echo ok`,
	})
	require.NoError(t, err)
	assert.Equal(t, "ok\n", string(res.Stdout))
}

type signalWriter struct {
	once    sync.Once
	written chan struct{}
}

func newSignalWriter() *signalWriter {
	return &signalWriter{written: make(chan struct{})}
}

func (w *signalWriter) Write(p []byte) (int, error) {
	w.once.Do(func() {
		close(w.written)
	})
	return len(p), nil
}

func (w *signalWriter) Written() <-chan struct{} {
	return w.written
}

func requireWrittenBeforeExit(
	t *testing.T,
	written <-chan struct{},
	done <-chan error,
) {
	t.Helper()

	select {
	case <-written:
	case err := <-done:
		require.NoError(t, err)
		t.Fatal("script exited before output was streamed")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("output was not streamed before script exit")
	}
}

var _ io.Writer = (*signalWriter)(nil)

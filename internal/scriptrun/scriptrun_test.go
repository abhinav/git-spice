package scriptrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
		Log:  silog.Nop(),
		Args: []string{"prog", "alpha", "beta"},
	}

	res, err := r.Run(t.Context(), &RunRequest{
		Script: `echo "$1-$2"`,
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
		Log:  silog.Nop(),
		Args: []string{"prog", "first", "second"},
	}

	res, err := r.Run(t.Context(), &RunRequest{
		Script: "#!/bin/sh\necho \"$1=$2\"\n",
	})
	require.NoError(t, err)
	assert.Equal(t, "first=second\n", string(res.Stdout))
}

func TestRunner_Run_emptyScript(t *testing.T) {
	r := &Runner{Log: silog.Nop()}

	_, err := r.Run(t.Context(), &RunRequest{})
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

func TestRunner_Run_nilLogger(t *testing.T) {
	r := &Runner{} // no logger

	res, err := r.Run(t.Context(), &RunRequest{
		Script: `echo ok`,
	})
	require.NoError(t, err)
	assert.Equal(t, "ok\n", string(res.Stdout))
}

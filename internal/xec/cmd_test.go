package xec

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog"
	"go.uber.org/mock/gomock"
)

func TestCommand(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	cmd := Command(ctx, log, "echo", "hello", "world")
	output, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "hello world\n", string(output))
}

func TestCmd_Args(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	t.Run("RetrieveArgs", func(t *testing.T) {
		cmd := Command(ctx, log, "echo", "arg1", "arg2", "arg3")
		assert.Equal(t, []string{"arg1", "arg2", "arg3"}, cmd.Args())
	})

	t.Run("ReplaceArgs", func(t *testing.T) {
		cmd := Command(ctx, log, "echo", "original")
		cmd.WithArgs("new", "args")

		assert.Equal(t, []string{"new", "args"}, cmd.Args())

		output, err := cmd.Output()
		require.NoError(t, err)
		assert.Equal(t, "new args\n", string(output))
	})
}

func TestCmd_WithDir(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	dir := t.TempDir()
	// Resolve symlinks since t.TempDir() may return a symlinked path.
	resolvedDir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	cmd := Command(ctx, log, "pwd").WithDir(dir)
	output, err := cmd.OutputChomp()
	require.NoError(t, err)
	assert.Equal(t, resolvedDir, output)
}

func TestCmd_WithLogPrefix(t *testing.T) {
	ctx := t.Context()
	var logBuffer bytes.Buffer
	log := silog.New(&logBuffer, &silog.Options{
		Level: silog.LevelDebug,
	})

	cmd := Command(ctx, log, "sh", "-c", "echo 'error message' >&2").
		WithLogPrefix("custom-prefix")

	require.NoError(t, cmd.Run())
	assert.Contains(t, logBuffer.String(), "custom-prefix: error message")
}

func TestCmd_WithStdout(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()
	var buf bytes.Buffer

	cmd := Command(ctx, log, "echo", "test output").
		WithStdout(&buf)

	require.NoError(t, cmd.Run())
	assert.Equal(t, "test output\n", buf.String())
}

func TestCmd_WithStderr(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()
	var buf bytes.Buffer

	cmd := Command(ctx, log, "sh", "-c", "echo 'stderr output' >&2").
		WithStderr(&buf)

	require.NoError(t, cmd.Run())
	assert.Equal(t, "stderr output\n", buf.String())
}

func TestCmd_WithStdin(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	t.Run("WithReader", func(t *testing.T) {
		reader := strings.NewReader("test input")
		cmd := Command(ctx, log, "cat").WithStdin(reader)

		output, err := cmd.Output()
		require.NoError(t, err)
		assert.Equal(t, "test input", string(output))
	})

	t.Run("WithString", func(t *testing.T) {
		cmd := Command(ctx, log, "cat").WithStdinString("test input")

		output, err := cmd.Output()
		require.NoError(t, err)
		assert.Equal(t, "test input", string(output))
	})
}

func TestCmd_AppendEnv(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	t.Run("SingleVar", func(t *testing.T) {
		cmd := Command(ctx, log, "sh", "-c", "echo $TEST_VAR").
			AppendEnv("TEST_VAR=test_value")

		output, err := cmd.OutputChomp()
		require.NoError(t, err)
		assert.Equal(t, "test_value", output)
	})

	t.Run("MultipleVars", func(t *testing.T) {
		cmd := Command(ctx, log, "sh", "-c", "echo $VAR1 $VAR2").
			AppendEnv("VAR1=value1", "VAR2=value2")

		output, err := cmd.OutputChomp()
		require.NoError(t, err)
		assert.Equal(t, "value1 value2", output)
	})
}

func TestCmd_Run(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	t.Run("Success", func(t *testing.T) {
		cmd := Command(ctx, log, "true")
		require.NoError(t, cmd.Run())
	})

	t.Run("Failure", func(t *testing.T) {
		cmd := Command(ctx, log, "false")
		require.Error(t, cmd.Run())
	})
}

func TestCmd_Output(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	t.Run("Success", func(t *testing.T) {
		cmd := Command(ctx, log, "echo", "test output")
		output, err := cmd.Output()
		require.NoError(t, err)
		assert.Equal(t, "test output\n", string(output))
	})

	t.Run("Failure", func(t *testing.T) {
		cmd := Command(ctx, log, "sh", "-c", "echo 'output'; exit 1")
		output, err := cmd.Output()
		assert.Error(t, err)
		// Output is still captured even on failure.
		assert.Equal(t, "output\n", string(output))
	})
}

func TestCmd_OutputChomp(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	t.Run("WithNewline", func(t *testing.T) {
		cmd := Command(ctx, log, "echo", "test")
		output, err := cmd.OutputChomp()
		require.NoError(t, err)
		assert.Equal(t, "test", output)
	})

	t.Run("WithoutNewline", func(t *testing.T) {
		cmd := Command(ctx, log, "printf", "test")
		output, err := cmd.OutputChomp()
		require.NoError(t, err)
		assert.Equal(t, "test", output)
	})
}

func TestCmd_StartWait(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	t.Run("Success", func(t *testing.T) {
		cmd := Command(ctx, log, "true")
		require.NoError(t, cmd.Start())
		require.NoError(t, cmd.Wait())
	})

	t.Run("Failure", func(t *testing.T) {
		cmd := Command(ctx, log, "false")
		require.NoError(t, cmd.Start())
		assert.Error(t, cmd.Wait())
	})
}

func TestCmd_Kill(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	cmd := Command(ctx, log, "sleep", "60")
	require.NoError(t, cmd.Start())
	require.NoError(t, cmd.Kill())
	assert.Error(t, cmd.Wait())
}

func TestCmd_StdoutPipe(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	cmd := Command(ctx, log, "echo", "test")
	pipe, err := cmd.StdoutPipe()
	require.NoError(t, err)
	defer func() { assert.NoError(t, pipe.Close()) }()

	require.NoError(t, cmd.Start())

	output, err := io.ReadAll(pipe)
	require.NoError(t, err)
	assert.Equal(t, "test\n", string(output))
}

func TestCmd_StdinPipe(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	var buf bytes.Buffer
	cmd := Command(ctx, log, "cat").WithStdout(&buf)

	pipe, err := cmd.StdinPipe()
	require.NoError(t, err)

	require.NoError(t, cmd.Start())

	_, err = pipe.Write([]byte("test input"))
	require.NoError(t, err)

	require.NoError(t, pipe.Close())
	require.NoError(t, cmd.Wait())

	assert.Equal(t, "test input", buf.String())
}

func TestCmd_Lines(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	cmd := Command(ctx, log,
		"printf", "%s\n%s\n%s\n",
		"line1", "line2", "line3",
	)

	var lines []string
	for line, err := range cmd.Lines() {
		require.NoError(t, err)
		lines = append(lines, string(line))
	}

	assert.Equal(t, []string{"line1", "line2", "line3"}, lines)
}

func TestCmd_Scan(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	cmd := Command(ctx, log, "echo", "word1 word2 word3")

	var words []string
	for word, err := range cmd.Scan(bufio.ScanWords) {
		require.NoError(t, err)
		words = append(words, string(word))
	}

	assert.Equal(t, []string{"word1", "word2", "word3"}, words)
}

func TestCmd_Scan_StartError(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()
	mockCtrl := gomock.NewController(t)

	mock := NewMockExecer(mockCtrl)
	mock.EXPECT().
		Start(gomock.Any()).
		Return(errors.New("start failed"))

	cmd := Command(ctx, log, "echo", "test").WithExecer(mock)

	// There must be only one entry: the error.
	for _, err := range cmd.Scan(bufio.ScanLines) {
		require.Error(t, err)
		assert.ErrorContains(t, err, "start")
	}
}

func TestCmd_CaptureStdout(t *testing.T) {
	ctx := t.Context()
	var logBuffer bytes.Buffer
	log := silog.New(&logBuffer, &silog.Options{
		Level: silog.LevelInfo,
	})

	cmd := Command(ctx, log, "sh", "-c", "echo 'stdout message'; exit 1").
		CaptureStdout()

	err := cmd.Run()
	require.Error(t, err)
	assert.ErrorContains(t, err, "stdout:")
	assert.ErrorContains(t, err, "stdout message")
}

func TestCmd_StderrHandling(t *testing.T) {
	ctx := t.Context()

	t.Run("DebugLevel_LogsStderr", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, &silog.Options{
			Level: silog.LevelDebug,
		})

		cmd := Command(ctx, log, "sh", "-c", "echo 'stderr message' >&2")
		require.NoError(t, cmd.Run())
		assert.Contains(t, logBuffer.String(), "stderr message")
	})

	t.Run("InfoLevel_CapturesStderrInError", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, &silog.Options{
			Level: silog.LevelInfo,
		})

		cmd := Command(ctx, log, "sh", "-c", "echo 'error message' >&2; exit 1")

		err := cmd.Run()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stderr:")
		assert.Contains(t, err.Error(), "error message")

		// Stderr should NOT be in logs.
		assert.NotContains(t, logBuffer.String(), "error message")
	})

	t.Run("InfoLevel_NoStderrOnSuccess", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, &silog.Options{
			Level: silog.LevelInfo,
		})

		cmd := Command(ctx, log, "sh", "-c", "echo 'stderr message' >&2")
		require.NoError(t, cmd.Run())

		// Stderr is discarded on success.
		assert.NotContains(t, logBuffer.String(), "stderr message")
	})
}

func TestCmd_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	log := silog.Nop()

	cmd := Command(ctx, log, "sleep", "60")
	require.NoError(t, cmd.Start())

	cancel()

	// Give the process a moment to be killed.
	time.Sleep(100 * time.Millisecond)

	assert.Error(t, cmd.Wait())
}

func TestCmd_WithExecer(t *testing.T) {
	ctx := t.Context()
	log := silog.Nop()

	t.Run("Run", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		mock := NewMockExecer(mockCtrl)
		mock.EXPECT().
			Run(gomock.Any()).
			Return(nil)

		cmd := Command(ctx, log, "echo", "test").WithExecer(mock)
		require.NoError(t, cmd.Run())
	})

	t.Run("Start", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		mock := NewMockExecer(mockCtrl)
		mock.EXPECT().
			Start(gomock.Any()).
			Return(nil)

		cmd := Command(ctx, log, "echo", "test").WithExecer(mock)
		require.NoError(t, cmd.Start())
	})

	t.Run("Wait", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		mock := NewMockExecer(mockCtrl)
		mock.EXPECT().
			Wait(gomock.Any()).
			Return(nil)

		cmd := Command(ctx, log, "echo", "test").WithExecer(mock)
		require.NoError(t, cmd.Wait())
	})

	t.Run("Kill", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		mock := NewMockExecer(mockCtrl)
		mock.EXPECT().
			Kill(gomock.Any()).
			Return(nil)

		cmd := Command(ctx, log, "echo", "test").WithExecer(mock)
		require.NoError(t, cmd.Kill())
	})

	t.Run("Output", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		expected := []byte("output")
		mock := NewMockExecer(mockCtrl)
		mock.EXPECT().
			Output(gomock.Any()).
			Return(expected, nil)

		cmd := Command(ctx, log, "echo", "test").WithExecer(mock)
		output, err := cmd.Output()
		require.NoError(t, err)
		assert.Equal(t, expected, output)
	})
}

package xec

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog"
)

func TestCmd_RunWithRetry(t *testing.T) {
	t.Run("SucceedsFirstTry", func(t *testing.T) {
		cmd := Command(t.Context(), silog.Nop(), "true").
			WithRetry(&RetryPolicy{
				Match:     func(error) bool { return true },
				Timeout:   time.Second,
				BaseDelay: 10 * time.Millisecond,
			})

		require.NoError(t, cmd.Run())
	})

	t.Run("RetriesOnMatch", func(t *testing.T) {
		// First attempt fails, second succeeds.
		attempt := 0
		cmd := Command(t.Context(), silog.Nop(),
			"sh", "-c", "exit 1").
			WithRetry(&RetryPolicy{
				Match: func(error) bool {
					attempt++
					return attempt <= 1
				},
				Timeout:   time.Second,
				BaseDelay: 10 * time.Millisecond,
			})

		// The second attempt still runs "exit 1"
		// so it fails, but Match returns false
		// so it's not retried.
		err := cmd.Run()
		assert.Error(t, err)
		assert.Equal(t, 2, attempt)
	})

	t.Run("NoRetryOnNonMatch", func(t *testing.T) {
		attempt := 0
		cmd := Command(t.Context(), silog.Nop(),
			"sh", "-c", "exit 1").
			WithRetry(&RetryPolicy{
				Match: func(error) bool {
					attempt++
					return false
				},
				Timeout:   time.Second,
				BaseDelay: 10 * time.Millisecond,
			})

		err := cmd.Run()
		assert.Error(t, err)
		assert.Equal(t, 1, attempt)
	})

	t.Run("ExhaustsTimeout", func(t *testing.T) {
		attempts := 0
		now := time.Now()
		cmd := Command(t.Context(), silog.Nop(),
			"sh", "-c", "exit 1").
			WithRetry(&RetryPolicy{
				Match: func(error) bool {
					attempts++
					return true
				},
				Timeout:   150 * time.Millisecond,
				BaseDelay: 50 * time.Millisecond,
				nowFunc:   func() time.Time { return now },
			})

		err := cmd.Run()
		assert.Error(t, err)
		// 50ms + 100ms = 150ms fills the timeout,
		// so we expect ~3 attempts
		// (first immediate, then 50ms, then 100ms).
		assert.GreaterOrEqual(t, attempts, 2)
	})

	// Verifies that the retry deadline starts
	// after the first command failure,
	// not before command execution.
	// A long-running command that exceeds the timeout
	// should still get at least one retry.
	t.Run("DeadlineStartsAfterFirstFailure", func(t *testing.T) {
		attempts := 0
		cmd := Command(t.Context(), silog.Nop(),
			"sh", "-c",
			// Sleep longer than the retry timeout,
			// then fail.
			"sleep 0.2; exit 1").
			WithRetry(&RetryPolicy{
				Match: func(error) bool {
					attempts++
					return true
				},
				// Timeout is shorter than
				// the command's execution time.
				Timeout:   100 * time.Millisecond,
				BaseDelay: 10 * time.Millisecond,
			})

		err := cmd.Run()
		assert.Error(t, err)
		// The command itself takes 200ms,
		// longer than the 100ms timeout.
		// Retry must still fire at least once
		// because the deadline is set
		// after the first failure.
		assert.GreaterOrEqual(t, attempts, 2,
			"should retry even when command "+
				"runs longer than timeout")
	})

	t.Run("RespectsContext", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		attempts := 0
		cmd := Command(ctx, silog.Nop(),
			"sh", "-c", "exit 1").
			WithRetry(&RetryPolicy{
				Match: func(error) bool {
					attempts++
					// Cancel after first attempt.
					cancel()
					return true
				},
				Timeout:   5 * time.Second,
				BaseDelay: time.Second,
			})

		err := cmd.Run()
		assert.Error(t, err)
		assert.Equal(t, 1, attempts)
	})

	t.Run("ZeroTimeoutDisablesRetry", func(t *testing.T) {
		attempts := 0
		cmd := Command(t.Context(), silog.Nop(),
			"sh", "-c", "exit 1").
			WithRetry(&RetryPolicy{
				Match: func(error) bool {
					attempts++
					return true
				},
				Timeout:   0,
				BaseDelay: 10 * time.Millisecond,
			})

		err := cmd.Run()
		assert.Error(t, err)
		assert.Equal(t, 0, attempts)
	})

	t.Run("NegativeTimeoutRetriesIndefinitely", func(t *testing.T) {
		attempts := 0
		cmd := Command(t.Context(), silog.Nop(),
			"sh", "-c", "exit 1").
			WithRetry(&RetryPolicy{
				Match: func(error) bool {
					attempts++
					// Stop matching after 3 attempts
					// to avoid infinite loop.
					return attempts < 3
				},
				Timeout:   -1,
				BaseDelay: 10 * time.Millisecond,
			})

		err := cmd.Run()
		assert.Error(t, err)
		assert.Equal(t, 3, attempts)
	})

	t.Run("SucceedsAfterTransientFailure", func(t *testing.T) {
		// Use a file to track state across processes.
		marker := t.TempDir() + "/marker"
		cmd := Command(t.Context(), silog.Nop(),
			"sh", "-c",
			// First call creates marker and fails;
			// second call sees marker and succeeds.
			"if [ -f "+marker+" ]; then exit 0; "+
				"else touch "+marker+"; exit 1; fi").
			WithRetry(&RetryPolicy{
				Match:     func(error) bool { return true },
				Timeout:   time.Second,
				BaseDelay: 10 * time.Millisecond,
			})

		require.NoError(t, cmd.Run())
	})

	t.Run("RetriesWithUpdatedArgs", func(t *testing.T) {
		marker := t.TempDir() + "/marker"
		cmd := Command(t.Context(), silog.Nop(),
			"sh", "-c", "exit 1").
			WithRetry(&RetryPolicy{
				Match:     func(error) bool { return true },
				Timeout:   time.Second,
				BaseDelay: 10 * time.Millisecond,
			})
		cmd.WithArgs(
			"-c",
			"if [ -f "+marker+" ]; then exit 0; "+
				"else touch "+marker+"; exit 1; fi",
		)

		require.NoError(t, cmd.Run())
	})
}

func TestCmd_OutputWithRetry(t *testing.T) {
	t.Run("SucceedsAfterTransientFailure", func(t *testing.T) {
		marker := t.TempDir() + "/marker"
		cmd := Command(t.Context(), silog.Nop(),
			"sh", "-c",
			"if [ -f "+marker+" ]; then echo ok; exit 0; "+
				"else touch "+marker+"; exit 1; fi").
			WithRetry(&RetryPolicy{
				Match:     func(error) bool { return true },
				Timeout:   time.Second,
				BaseDelay: 10 * time.Millisecond,
			})

		out, err := cmd.OutputChomp()
		require.NoError(t, err)
		assert.Equal(t, "ok", out)
	})

	t.Run("MatchesExitError", func(t *testing.T) {
		matched := false
		cmd := Command(t.Context(), silog.Nop(),
			"sh", "-c",
			"echo 'index.lock' >&2; exit 1").
			WithRetry(&RetryPolicy{
				Match: func(err error) bool {
					var exitErr *exec.ExitError
					if errors.As(err, &exitErr) {
						matched = true
					}
					return false
				},
				Timeout:   time.Second,
				BaseDelay: 10 * time.Millisecond,
			})

		_, err := cmd.Output()
		assert.Error(t, err)
		assert.True(t, matched,
			"Match should receive *exec.ExitError")
	})
}

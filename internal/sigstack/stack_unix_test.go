//go:build unix

package sigstack

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _testBinary string

func TestMain(m *testing.M) {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr,
			"Failed to get executable path:", err)
		os.Exit(1)
	}
	_testBinary = exe

	os.Exit(m.Run())
}

func TestStack_Notify(t *testing.T) {
	var s Stack

	ch := make(chan Signal, 1)
	s.Notify(ch, syscall.SIGUSR1)
	defer s.Stop(ch)

	sendSignal(t, syscall.SIGUSR1)
	assertRecv(t, ch, time.Second)

	t.Run("StopCleansUp", func(t *testing.T) {
		s.Stop(ch)

		// After Stop, the signal state should be cleaned up.
		s.mu.Lock()
		defer s.mu.Unlock()
		assert.NotContains(t, s.states, syscall.SIGUSR1,
			"signal state should be removed after Stop")
	})
}

func TestStack_Notify_stackOrdering(t *testing.T) {
	var s Stack

	chA := make(chan Signal, 1)
	s.Notify(chA, syscall.SIGUSR1)
	defer s.Stop(chA)

	chB := make(chan Signal, 1)
	s.Notify(chB, syscall.SIGUSR1)
	defer s.Stop(chB)

	// With both registered, only B (topmost) should receive.
	sendSignal(t, syscall.SIGUSR1)
	assertRecv(t, chB, time.Second)
	assert.Empty(t, chA)

	// Stop B, send again — A should now receive.
	t.Run("StopTopmost", func(t *testing.T) {
		s.Stop(chB)

		sendSignal(t, syscall.SIGUSR1)
		assertRecv(t, chA, time.Second)
		assert.Empty(t, chB)
	})
}

func TestStack_Notify_multipleSignals(t *testing.T) {
	var s Stack

	ch := make(chan Signal, 2)
	s.Notify(ch, syscall.SIGUSR1, syscall.SIGUSR2)
	defer s.Stop(ch)

	sendSignal(t, syscall.SIGUSR1)
	assertRecv(t, ch, time.Second)

	sendSignal(t, syscall.SIGUSR2)
	assertRecv(t, ch, time.Second)
}

func TestStack_Stop_idempotent(t *testing.T) {
	var s Stack

	ch := make(chan Signal, 1)
	s.Notify(ch, syscall.SIGUSR1)
	s.Stop(ch)

	// Second Stop should not panic.
	assert.NotPanics(t, func() { s.Stop(ch) })
}

func TestStack_Stop_outOfOrder(t *testing.T) {
	var s Stack

	chA := make(chan Signal, 1)
	s.Notify(chA, syscall.SIGUSR1)
	defer s.Stop(chA)

	chB := make(chan Signal, 1)
	s.Notify(chB, syscall.SIGUSR1)
	defer s.Stop(chB)

	// Stop A (non-topmost); B should still receive.
	s.Stop(chA)

	sendSignal(t, syscall.SIGUSR1)
	assertRecv(t, chB, time.Second)
	assert.Empty(t, chA)
}

func TestStack_Notify_noopAbsorbs(t *testing.T) {
	var s Stack

	// A registered channel absorbs the signal
	// without terminating the process.
	ch := make(chan Signal, 1)
	s.Notify(ch, syscall.SIGUSR1)
	defer s.Stop(ch)

	sendSignal(t, syscall.SIGUSR1)

	// If we reach here, the signal was absorbed.
}

func TestStack_Stop_unregistersHandler(t *testing.T) {
	if os.Getenv("INSIDE_TEST") == "1" {
		// Subprocess mode:
		// register a handler, wait for a signal,
		// then unregister and block forever.
		var s Stack
		go func() {
			ch := make(chan Signal, 1)
			s.Notify(ch, syscall.SIGINT)

			fmt.Println("ready")
			<-ch

			s.Stop(ch)
			fmt.Println("stopped")
		}()

		select {} // block forever (will be killed by SIGINT)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, _testBinary, "-test.run=^"+t.Name()+"$")
	cmd.Env = append(os.Environ(), "INSIDE_TEST=1")

	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	scanner := bufio.NewScanner(stdout)

	// Wait for the subprocess to register its handler.
	require.True(t, scanner.Scan(), "expected 'ready' line")
	assert.Equal(t, "ready", scanner.Text())

	// First SIGINT: handled by the Stack.
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))

	// Wait for the subprocess to call Stop.
	require.True(t, scanner.Scan(), "expected 'stopped' line")
	assert.Equal(t, "stopped", scanner.Text())

	// Second SIGINT: handler is unregistered,
	// so the default action (terminate) should apply.
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))

	err = cmd.Wait()
	require.Error(t, err, "subprocess should have been killed")

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)

	status, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok, "expected syscall.WaitStatus")
	assert.Equal(t, syscall.SIGINT,
		status.Signal(),
		"subprocess should have been killed by SIGINT",
	)
}

func assertRecv(t testing.TB, ch <-chan Signal, timeout time.Duration) {
	t.Helper()

	select {
	case <-ch:
		// ok
	case <-time.After(timeout):
		t.Fatal("timed out waiting for signal")
	}
}

func sendSignal(t *testing.T, sig syscall.Signal) {
	t.Helper()

	require.NoError(t,
		syscall.Kill(syscall.Getpid(), sig),
	)
}

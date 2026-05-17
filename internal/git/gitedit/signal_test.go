//go:build unix

package gitedit

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/sigstack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

// TestIntegrationEditor_signalAbsorbed verifies that
// SIGINT is absorbed while the editor is running,
// matching Git's editor.c behavior.
//
// The test uses a subprocess re-exec pattern
// with a slow fake editor (slowedit) for coordination:
//
//	Parent test
//	  │
//	  ├─ spawns subprocess (test binary with env var)
//	  │    │
//	  │    ├─ creates git repo
//	  │    ├─ creates Editor with real sigstack.Stack
//	  │    ├─ calls Editor.CommitMessage
//	  │    │    │
//	  │    │    ├─ Signals.Notify(sigc, SIGINT, SIGQUIT)
//	  │    │    ├─ runs slowedit
//	  │    │    │    │
//	  │    │    │    ├─ writes ready sentinel
//	  │    │    │    ├─ polls for continue sentinel
//	  │    │    │    ├─ writes commit message
//	  │    │    │    └─ exits
//	  │    │    ├─ Signals.Stop(sigc)
//	  │    │    └─ returns message
//	  │    ├─ prints message to stdout
//	  │    └─ exits 0
//	  │
//	  ├─ polls for ready sentinel
//	  ├─ sends SIGINT ◄── absorbed by sigstack
//	  ├─ writes continue sentinel
//	  ├─ waits for subprocess exit
//	  └─ asserts: exit 0, correct message on stdout
func TestIntegrationEditor_signalAbsorbed(t *testing.T) {
	if os.Getenv("_GITEDIT_SIGNAL_TEST") == "1" {
		os.Exit(signalTestSubprocess())
		return
	}

	exe, err := os.Executable()
	require.NoError(t, err)

	// Set up coordination files.
	coordDir := t.TempDir()
	readyPath := filepath.Join(coordDir, "ready")
	contPath := filepath.Join(coordDir, "continue")
	givePath := filepath.Join(coordDir, "give")

	require.NoError(t,
		os.WriteFile(
			givePath,
			[]byte("signal test message\n"),
			0o644,
		),
	)

	// Build the fixture script for the subprocess.
	fixtureScript := text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-01-01T00:00:00Z'

		git init
		git commit --allow-empty -m 'Initial commit'
	`)

	cmd := exec.Command(exe,
		"-test.run=^TestIntegrationEditor_signalAbsorbed$",
		"-test.v",
	)
	cmd.Env = append(os.Environ(),
		"_GITEDIT_SIGNAL_TEST=1",
		"_GITEDIT_FIXTURE="+fixtureScript,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=",
		"GIT_EDITOR=slowedit",
		"SLOWEDIT_READY="+readyPath,
		"SLOWEDIT_CONTINUE="+contPath,
		"SLOWEDIT_GIVE="+givePath,
	)

	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Start())

	// Wait for slowedit to signal readiness.
	deadline := time.After(10 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for ready sentinel")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Send SIGINT — should be absorbed by the signal stack.
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	time.Sleep(50 * time.Millisecond)

	// Tell slowedit to finish.
	require.NoError(t,
		os.WriteFile(contPath, nil, 0o644),
	)

	// Read subprocess stdout for the commit message.
	scanner := bufio.NewScanner(stdout)
	var gotMessage string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "_MSG_START_" {
			if scanner.Scan() {
				gotMessage = scanner.Text()
			}
			break
		}
	}

	err = cmd.Wait()
	require.NoError(t, err,
		"subprocess should exit 0 "+
			"(signal must be absorbed)")
	assert.Equal(t, "signal test message", gotMessage)
}

// signalTestSubprocess runs inside the re-exec'd test binary.
// It creates a real git repo, configures an Editor
// with a real sigstack.Stack, and prints the resulting message.
func signalTestSubprocess() (exitCode int) {
	fixture, err := gittest.LoadFixtureScript(
		[]byte(os.Getenv("_GITEDIT_FIXTURE")),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load fixture: %v\n", err)
		return 1
	}
	defer fixture.Cleanup()

	ctx := context.Background()
	repo, err := git.Open(
		ctx, fixture.Dir(),
		git.OpenOptions{Log: silog.Nop()},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open repo: %v\n", err)
		return 1
	}

	var signals sigstack.Stack
	editor := &Editor{
		Repository: repo,
		Signals:    &signals,
		Log:        silog.Nop(),
	}

	var buf bytes.Buffer
	if err := editor.EditCommitMessage(
		ctx,
		strings.NewReader("placeholder"),
		&buf,
		nil,
	); err != nil {
		fmt.Fprintf(os.Stderr, "commit message: %v\n", err)
		return 1
	}

	// Print a marker followed by the message
	// so the parent can reliably parse it.
	fmt.Println("_MSG_START_")
	fmt.Print(buf.String())
	return 0
}

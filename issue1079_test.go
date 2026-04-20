package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/xec"
)

// https://github.com/abhinav/git-spice/issues/1079.
//
// After building a linear stack of tracked branches:
//
//   - advance `main` by one commit
//   - start a bash session attached to a pty
//   - run "gs repo restack" in the foreground
//   - type "e" while restack is still running
//   - wait for restack to finish and the prompt to return
//   - type "cho $VAR\n"
//
// This SHOULD run "echo $VAR", and print the random value assigned to it.
// If the leading "e" is lost, the result is "cho $VAR", which isn't a command.
func TestIssue1079_bashPTYLosesFirstTypedByte(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creack/pty does not work on Windows")
	}

	const (
		// Number of branches in the tracked stack.
		NumBranches = 5

		// Number of times to try to reproduce the issue.
		NumAttempts = 5

		// Time to wait for the interactive bash session to start.
		BashTimeout = 5 * time.Second

		// Delay after starting restack but before sending the "e".
		FirstLetterDelay = 50 * time.Millisecond

		// Maximum time to wait for restack to finish.
		RestackTimeout = 10 * time.Second

		// Time to wait for the probe output.
		ProbeTimeout = 2 * time.Second

		// How often to poll the PTY output for expected markers.
		PollInterval = 10 * time.Millisecond

		PS1 = "> " // bash prompt
	)

	testBin, err := os.Executable()
	require.NoError(t, err)

	// Reuse the current test binary as a `gs` command
	// by symlinking it into a temporary PATH entry.
	// TestMain routes the `gs` helper name back into main().
	binDir := filepath.Join(t.TempDir(), "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.Symlink(testBin, filepath.Join(binDir, "gs")))

	// Set up an isolated Git environment.
	homeDir := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".config"), 0o755))
	t.Setenv("PATH", binDir+string(filepath.ListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("TERM", "dumb")

	logger := silogtest.New(t)

	// Build the tracked stack once.
	// Later attempts only move trunk forward
	// so the same repository can be reused
	// without paying the setup cost repeatedly.
	repoDir := filepath.Join(t.TempDir(), "repo")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))

	gs := func(t *testing.T, args ...string) {
		t.Helper()

		cmd := xec.Command(t.Context(), logger, "gs", args...).
			WithDir(repoDir).
			WithStdout(t.Output()).
			WithStderr(t.Output())
		require.NoError(t, cmd.Run(), "gs: %q", cmd.Args)
	}

	git := func(t *testing.T, args ...string) {
		t.Helper()

		cmd := xec.Command(t.Context(), logger, "git", args...).
			WithDir(repoDir).
			WithStdout(t.Output()).
			WithStderr(t.Output())
		require.NoError(t, cmd.Run(), "git: %q", cmd.Args)
	}

	git(t, "init", "-b", "main")
	git(t, "config", "commit.gpgsign", "false")

	filePath := filepath.Join(repoDir, "file.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("root\n"), 0o644))
	git(t, "add", "file.txt")
	git(t, "commit", "-m", "root")

	gs(t, "repo", "init", "--trunk", "main", "--no-prompt")

	// Create a linear stack:
	// main <- b1 <- b2 <- b3 <- b4 <- b5
	//
	// Each branch adds one commit so later restacks have a predictable shape.
	for i := 1; i <= NumBranches; i++ {
		name := fmt.Sprintf("b%d", i)
		require.NoError(t,
			os.WriteFile(filePath, fmt.Appendf(nil, "branch %d\n", i), 0o644))
		git(t, "add", "file.txt")
		gs(t, "branch", "create", name, "-m", name)
	}

	for i := 1; i <= NumAttempts; i++ {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			// Move trunk ahead one commit so the stack
			// needs to be restacked again.
			gs(t, "trunk")

			attemptFile := filepath.Join(repoDir, fmt.Sprintf("trunk-%d.txt", i))
			require.NoError(t,
				os.WriteFile(attemptFile, fmt.Appendf(nil, "attempt %d\n", i), 0o644))
			git(t, "add", filepath.Base(attemptFile))
			git(t, "commit", "-m", fmt.Sprintf("trunk %d", i))
			git(t, "checkout", "-")
			t.Logf("stack ready for restacking")

			probeText := rand.Text()
			t.Logf("probe text: %q", probeText)

			shellCmd := exec.Command("bash", "--noprofile", "--norc", "-i")
			shellCmd.Dir = repoDir
			shellCmd.Env = append(os.Environ(),
				"PS1="+PS1,
				// dumb terminal so output is easy to match
				// without extra control characters.
				"TERM=dumb",
				// Pass probe text as an environment variable
				// so it's not shown in the "echo $VAR" command itself.
				"VAR="+probeText,
			)

			// Run interactive bash inside a real PTY. The bug depends on the shell
			// being connected to an actual terminal, not plain pipes.
			t.Logf("launching interactive bash PTY")
			ptyFile, err := pty.StartWithSize(shellCmd, &pty.Winsize{
				Rows: 40,
				Cols: 80,
			})
			require.NoError(t, err)
			defer func() {
				_ = shellCmd.Process.Kill()
				_ = ptyFile.Close()
				_ = shellCmd.Wait()
			}()

			var buffer lockedBuffer
			go func() {
				var chunk [4096]byte
				for {
					n, err := ptyFile.Read(chunk[:])
					if n > 0 {
						_, _ = buffer.Write(chunk[:n])
					}
					if err != nil {
						return
					}
				}
			}()
			defer func() {
				if t.Failed() {
					t.Logf("buffer:\n%s", buffer.String())
				}
			}()

			awaitBufferContains := func(needle string, timeout time.Duration) {
				t.Helper()

				require.Eventually(t, func() bool {
					return buffer.Contains([]byte(needle))
				}, timeout, PollInterval, "timed out waiting for: %q", needle)
			}

			// Wait for the PS1 prompt in the output.
			t.Logf("wait for initial bash prompt: %q", PS1)
			awaitBufferContains(PS1, BashTimeout)
			t.Logf("initial prompt ready")

			// Request restack in the foreground.
			buffer.Reset()
			t.Logf("start `gs repo restack`")
			_, err = io.WriteString(ptyFile, "gs repo restack\n")
			require.NoError(t, err)

			// This is the race window.
			// Sleep only long enough for restack to enter the
			// foreground work that triggers the bug,
			// then inject a single `e`
			// as if the user began typing the next command early.
			//
			// The value was determined through trial and error;
			// the issue reproduced reliably locally at 50ms.
			time.Sleep(FirstLetterDelay)
			t.Logf("injecting first byte")
			_, err = io.WriteString(ptyFile, "e")
			require.NoError(t, err)

			// Wait for restack to finish
			// and for bash to present the next prompt.
			restackMessage := fmt.Sprintf("Restacked %d branches\n%v", NumBranches, PS1)
			awaitBufferContains(restackMessage, RestackTimeout)
			t.Logf("restack finished and prompt returned")

			// Bash should still have the previously typed `e` buffered.
			// Send the remainder of the command.
			buffer.Reset()
			t.Logf("sending probe tail")
			_, err = io.WriteString(ptyFile, "cho $VAR\n")
			require.NoError(t, err)

			// Wait only for the probe result.
			// If the bug is present,
			// bash will run `cho PROBE`
			// and report `command not found`.
			// If fixed, it will print the probe text.
			t.Logf("waiting for probe output")
			awaitBufferContains(probeText, ProbeTimeout)
			output := buffer.String()
			t.Logf("probe output:\n%s", output)
			assert.NotContains(t, output, "command not found")
			assert.Contains(t, output, probeText)
		})
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (n int, err error) {
	n = len(p)
	// Get rid of CRs to simplify output matching.
	p = bytes.ReplaceAll(p, []byte{'\r'}, nil)

	b.mu.Lock()
	defer b.mu.Unlock()
	_, _ = b.buf.Write(p)
	return n, nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

func (b *lockedBuffer) Contains(bs []byte) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	return bytes.Contains(b.buf.Bytes(), bs)
}

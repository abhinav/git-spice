package xec

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEditCommand_setsGitSpiceExec(t *testing.T) {
	// Subprocess mode: modify file and verify GIT_SPICE is set.
	if os.Getenv("INSIDE_TEST") == "1" {
		flag.Parse()
		args := flag.Args()
		if len(args) == 0 {
			fmt.Fprintf(os.Stderr, "no file provided")
			os.Exit(1)
		}

		body := "GIT_SPICE=" + os.Getenv("GIT_SPICE")
		if err := os.WriteFile(args[0], []byte(body), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write file: %v\n", err)
			os.Exit(1)
		}

		os.Exit(0)
	}

	t.Run("SimpleEditor", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "test.txt")
		require.NoError(t, os.WriteFile(tmpFile, []byte(""), 0o644))

		cmd := EditCommand(_testBinary, "-test.run", "^"+t.Name()+"$", tmpFile)
		cmd.Env = append(cmd.Env, "INSIDE_TEST=1")

		require.NoError(t, cmd.Run())

		body, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		assert.Equal(t, "GIT_SPICE=1", string(body))
	})

	t.Run("ShellEditor", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "test.txt")
		require.NoError(t, os.WriteFile(tmpFile, []byte(""), 0o644))

		cmd := EditCommand(fmt.Sprintf("%q -test.run '^%s$'", _testBinary, t.Name()), tmpFile)
		cmd.Env = append(cmd.Env, "INSIDE_TEST=1")

		require.NoError(t, cmd.Run())

		body, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		assert.Equal(t, "GIT_SPICE=1", string(body))
	})
}

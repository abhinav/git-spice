package xec

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEditCommand_setsGitSpiceExec(t *testing.T) {
	// Subprocess mode: print GIT_SPICE value and exit.
	if os.Getenv("INSIDE_TEST") == "1" {
		fmt.Print(os.Getenv("GIT_SPICE"))
		os.Exit(0)
	}

	t.Run("SimpleEditor", func(t *testing.T) {
		cmd := EditCommand(_testBinary, "-test.run", "^"+t.Name()+"$")
		cmd.Env = append(cmd.Env, "INSIDE_TEST=1")

		output, err := cmd.Output()
		require.NoError(t, err)
		assert.Equal(t, "1", string(output))
	})

	t.Run("ShellEditor", func(t *testing.T) {
		cmd := EditCommand(fmt.Sprintf("%q -test.run '^%s$'", _testBinary, t.Name()))
		cmd.Env = append(cmd.Env, "INSIDE_TEST=1")
		cmd.Stderr = t.Output()

		output, err := cmd.Output()
		require.NoError(t, err)
		assert.Equal(t, "1", string(output))
	})
}

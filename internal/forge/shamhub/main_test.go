package shamhub

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	name := filepath.Base(os.Args[0])
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	}

	if name == "git" {
		// Failure-injection tests invoke this binary as Git.
		// Delegate all but the selected subcommand to the real Git so range
		// preparation and unchanged-state assertions still use real repositories.
		rejected := os.Getenv("SHAMHUB_TEST_REJECT_GIT_COMMAND")
		for _, arg := range os.Args[1:] {
			if arg == rejected {
				os.Exit(1)
			}
		}

		cmd := exec.Command(os.Getenv("SHAMHUB_TEST_REAL_GIT"), os.Args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
				os.Exit(exitErr.ExitCode())
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	m.Run()
}

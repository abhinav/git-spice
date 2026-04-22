package forge

import (
	"fmt"
	"os"
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
		if os.Getenv("GS_GCM_TEST_FAKE_GIT_MODE") == "gcm-fill" {
			fakeGitCredentialFill()
			os.Exit(0)
		}

		_, _ = os.Stderr.WriteString(
			"fake git invoked without configured helper mode\n")
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func fakeGitCredentialFill() {
	if len(os.Args) != 3 || os.Args[1] != "credential" || os.Args[2] != "fill" {
		_, _ = os.Stderr.WriteString("unexpected args\n")
		os.Exit(1)
	}

	if os.Getenv("GIT_TERMINAL_PROMPT") != "0" {
		_, _ = os.Stderr.WriteString(
			"GIT_TERMINAL_PROMPT was not disabled\n")
		os.Exit(1)
	}

	fmt.Print(`protocol=https
host=github.com
username=test-user
password=test-token
`)
}

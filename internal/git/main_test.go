package git

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"go.abhg.dev/gs/internal/mockedit"
)

func TestMain(m *testing.M) {
	name := filepath.Base(os.Args[0])
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	}

	if name == "git" {
		switch os.Getenv("GIT_ISSUE_1083_HELPER") {
		case "1":
			gitIssue1083()
			os.Exit(0)
		case "rebase-recovery-failure":
			gitRebaseRecoveryFailure()
			os.Exit(0)
		default:
			_, _ = os.Stderr.WriteString(
				"fake git invoked without configured helper mode\n")
			os.Exit(1)
		}
	}

	testscript.Main(m, map[string]func(){
		// mockedit <input>:
		"mockedit": mockedit.Main,
	})
}

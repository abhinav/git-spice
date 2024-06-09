package git_test

import (
	"os"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"go.abhg.dev/gs/internal/mockedit"
)

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		// mockedit <input>:
		"mockedit": mockedit.Main,
	}))
}

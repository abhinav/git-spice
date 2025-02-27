package git_test

import (
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"go.abhg.dev/gs/internal/mockedit"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		// mockedit <input>:
		"mockedit": mockedit.Main,
	})
}

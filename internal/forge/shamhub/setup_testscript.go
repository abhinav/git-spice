//go:build script

package shamhub

import (
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

type (
	shamHubKey struct{}
	shamHubEnv struct {
		t  testing.TB
		sh *ShamHub
	}
)

// SetupCmd implements the shamhub-setup command for test scripts.
type SetupCmd struct{}

// Setup installs per-script ShamHub state.
func (c *SetupCmd) Setup(t testing.TB, e *testscript.Env) {
	e.Values[shamHubKey{}] = &shamHubEnv{t: t}
}

// Run starts a ShamHub server and exports its connection details.
func (c *SetupCmd) Run(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 0 {
		ts.Fatalf("usage: shamhub-setup")
	}

	env := ts.Value(shamHubKey{}).(*shamHubEnv)
	if env.sh != nil {
		ts.Fatalf("ShamHub already initialized")
	}

	sh, err := New(Config{
		Log: silogtest.New(env.t),
	})
	if err != nil {
		ts.Fatalf("create ShamHub: %s", err)
	}
	ts.Defer(func() {
		if err := sh.Close(); err != nil {
			ts.Logf("close ShamHub: %s", err)
		}
	})
	env.sh = sh

	ts.Logf("Set up ShamHub:\n"+
		"  API URL  = %s\n"+
		"  Git URL  = %s\n"+
		"  Git root = %s",
		sh.APIURL(),
		sh.GitURL(),
		sh.GitRoot(),
	)
	ts.Setenv("SHAMHUB_API_URL", sh.APIURL())
	ts.Setenv("SHAMHUB_URL", sh.GitURL())
	ts.Setenv("SHAMHUB_ADMIN_TOKEN", sh.AdminToken())
}

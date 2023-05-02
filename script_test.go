package main

import (
	"flag"
	"net/mail"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

var _update = flag.Bool("update", false, "update golden files")

func TestMain(m *testing.M) {
	testscript.RunMain(m, map[string]func() int{
		"gs": func() int {
			main()
			return 0
		},
	})
}

func TestScript(t *testing.T) {
	defaultGitConfig := map[string]string{
		"init.defaultBranch": "main",
	}

	testscript.Run(t, testscript.Params{
		Dir:                filepath.Join("testdata", "script"),
		UpdateScripts:      *_update,
		RequireUniqueNames: true,
		Setup: func(e *testscript.Env) error {
			// We can set Git configuration values by setting
			// GIT_CONFIG_KEY_<n>, GIT_CONFIG_VALUE_<n> and GIT_CONFIG_COUNT.
			var numCfg int
			for k, v := range defaultGitConfig {
				n := strconv.Itoa(numCfg)
				e.Setenv("GIT_CONFIG_KEY_"+n, k)
				e.Setenv("GIT_CONFIG_VALUE_"+n, v)
				numCfg++
			}
			e.Setenv("GIT_CONFIG_COUNT", strconv.Itoa(numCfg))

			return nil
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"git": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg {
					ts.Fatalf("usage: git <args>")
				}
				ts.Check(ts.Exec("git", args...))
			},
			"as": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) != 1 {
					ts.Fatalf("usage: as 'User Name <user@example.com>'")
				}

				addr, err := mail.ParseAddress(args[0])
				if err != nil {
					ts.Fatalf("invalid email address: %s", err)
				}

				ts.Setenv("GIT_AUTHOR_NAME", addr.Name)
				ts.Setenv("GIT_AUTHOR_EMAIL", addr.Address)
				ts.Setenv("GIT_COMMITTER_NAME", addr.Name)
				ts.Setenv("GIT_COMMITTER_EMAIL", addr.Address)
			},
			"at": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) != 1 {
					ts.Fatalf("usage: at <YYYY-MM-DDTHH:MM:SS>")
				}

				t, err := time.Parse(time.RFC3339, args[0])
				if err != nil {
					ts.Fatalf("invalid time: %s", err)
				}

				gitTime := t.Format(time.RFC3339)
				ts.Setenv("GIT_AUTHOR_DATE", gitTime)
				ts.Setenv("GIT_COMMITTER_DATE", gitTime)
			},
		},
	})
}

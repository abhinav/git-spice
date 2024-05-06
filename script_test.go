package main

import (
	"flag"
	"net/mail"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
	"go.abhg.dev/gs/internal/termtest"
)

var _update = flag.Bool("update", false, "update golden files")

func TestMain(m *testing.M) {
	testscript.RunMain(m, map[string]func() int{
		"gs": func() int {
			main()
			return 0
		},
		// with-term file -- cmd [args ...]
		//
		// Runs the given command inside a terminal emulator,
		// using the file to drive interactions with it.
		// See [termtest.WithTerm] for supported commands.
		"with-term": termtest.WithTerm,
	})
}

func TestScript(t *testing.T) {
	defaultGitConfig := map[string]string{
		"init.defaultBranch": "main",
	}

	defaultEnv := make(map[string]string)
	// We can set Git configuration values by setting
	// GIT_CONFIG_KEY_<n>, GIT_CONFIG_VALUE_<n> and GIT_CONFIG_COUNT.
	var numCfg int
	for k, v := range defaultGitConfig {
		n := strconv.Itoa(numCfg)
		defaultEnv["GIT_CONFIG_KEY_"+n] = k
		defaultEnv["GIT_CONFIG_VALUE_"+n] = v
		numCfg++
	}
	defaultEnv["GIT_CONFIG_COUNT"] = strconv.Itoa(numCfg)

	// Add a default author to all commits.
	// Tests can override with 'as' and 'at'.
	defaultEnv["GIT_AUTHOR_NAME"] = "Test"
	defaultEnv["GIT_AUTHOR_EMAIL"] = "test@example.com"
	defaultEnv["GIT_COMMITTER_NAME"] = "Test"
	defaultEnv["GIT_COMMITTER_EMAIL"] = "test@example.com"

	testscript.Run(t, testscript.Params{
		Dir:                filepath.Join("testdata", "script"),
		UpdateScripts:      *_update,
		RequireUniqueNames: true,
		Setup: func(e *testscript.Env) error {
			for k, v := range defaultEnv {
				e.Setenv(k, v)
			}

			return nil
		},
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"git": cmdGit,
			"as":  cmdAs,
			"at":  cmdAt,
		},
	})
}

func cmdGit(ts *testscript.TestScript, neg bool, args []string) {
	err := ts.Exec("git", args...)
	if neg {
		if err == nil {
			ts.Fatalf("unexpected success, expected failure")
		}
	} else {
		ts.Check(err)
	}
}

func cmdAs(ts *testscript.TestScript, neg bool, args []string) {
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
}

func cmdAt(ts *testscript.TestScript, neg bool, args []string) {
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
}

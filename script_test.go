package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/rogpeppe/go-internal/diff"
	"github.com/rogpeppe/go-internal/testscript"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/shamhub"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/mockedit"
	"go.abhg.dev/gs/internal/termtest"
)

var _update = flag.Bool("update", false, "update golden files")

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"gs": func() int {
			logger := log.NewWithOptions(os.Stderr, log.Options{
				Level: log.DebugLevel,
			})

			forge.Register(&shamhub.Forge{Log: logger})
			main()
			return 0
		},
		"mockedit": mockedit.Main,
		// "true" is a no-op command that always succeeds.
		"true": func() int { return 0 },
		// with-term file -- cmd [args ...]
		//
		// Runs the given command inside a terminal emulator,
		// using the file to drive interactions with it.
		// See [termtest.WithTerm] for supported commands.
		"with-term": termtest.WithTerm,
	}))
}

func TestScript(t *testing.T) {
	defaultEnv := gittest.DefaultConfig().EnvMap()
	defaultEnv["EDITOR"] = "mockedit"

	// Add a default author to all commits.
	// Tests can override with 'as' and 'at'.
	defaultEnv["GIT_AUTHOR_NAME"] = "Test"
	defaultEnv["GIT_AUTHOR_EMAIL"] = "test@example.com"
	defaultEnv["GIT_COMMITTER_NAME"] = "Test"
	defaultEnv["GIT_COMMITTER_EMAIL"] = "test@example.com"

	var shamhubCmd shamhub.Cmd

	testscript.Run(t, testscript.Params{
		Dir:                filepath.Join("testdata", "script"),
		UpdateScripts:      *_update,
		RequireUniqueNames: true,
		Setup: func(e *testscript.Env) error {
			for k, v := range defaultEnv {
				e.Setenv(k, v)
			}

			shamhubCmd.Setup(t, e)
			return nil
		},
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"git": gittest.CmdGit,
			"as":  gittest.CmdAs,
			"at": func(ts *testscript.TestScript, b bool, s []string) {
				gittest.CmdAt(ts, b, s)

				// Set the Git-speciifc environment variables,
				// as well as git-spice's own GIT_SPICE_NOW.
				// Tests that want a different behavior for log
				// can set GIT_SPICE_NOW to a different value.
				ts.Setenv("GIT_SPICE_NOW", ts.Getenv("GIT_COMMITTER_DATE"))
			},
			"cmpenvJSON": cmdCmpenvJSON,
			"shamhub":    shamhubCmd.Run,
		},
	})
}

func cmdCmpenvJSON(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) != 2 {
		ts.Fatalf("usage: cmpjson file1 file2")
	}
	name1, name2 := args[0], args[1]

	data1 := []byte(ts.ReadFile(name1))
	data2, err := os.ReadFile(ts.MkAbs(name2))
	ts.Check(err)

	// Expand environment variables in data2.
	data2 = []byte(os.Expand(string(data2), ts.Getenv))

	var json1, json2 any
	ts.Check(json.Unmarshal(data1, &json1))
	ts.Check(json.Unmarshal(data2, &json2))

	if reflect.DeepEqual(json1, json2) == !neg {
		// Matches expectation.
		return
	}

	prettyJSON1, err := json.MarshalIndent(json1, "", "  ")
	ts.Check(err)

	if neg {
		ts.Logf("%s", prettyJSON1)
		ts.Fatalf("%s and %s do not differ", name1, name2)
		return
	}

	prettyJSON2, err := json.MarshalIndent(json2, "", "  ")
	ts.Check(err)

	unifiedDiff := diff.Diff(name1, prettyJSON1, name2, prettyJSON2)
	ts.Logf("%s", unifiedDiff)
	ts.Fatalf("%s and %s differ", name1, name2)
}

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/diff"
	"github.com/rogpeppe/go-internal/testscript"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/browser"
	"go.abhg.dev/gs/internal/browser/browsertest"
	"go.abhg.dev/gs/internal/forge/shamhub"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/mockedit"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/secret/secrettest"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/termtest"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/uitest"
)

var (
	_update = flag.Bool("update", false, "update golden files")
	_debug  = flag.Bool("debug", false, "enable debug logging")

	_shardIndex = flag.Int("shard-index", 0, "index of the test shard to run")
	_shardCount = flag.Int("shard-count", 1, "total number of test shards")
)

func TestMain(m *testing.M) {
	// Always override the secret stash with a memory stash
	// so that tests don't accidentally use the system keyring.
	_secretStash = new(secret.MemoryStash)

	// Always override the browser launcher with a no-op launcher
	// so tests don't accidentally open a browser.
	_browserLauncher = new(browser.Noop)

	testscript.Main(m, map[string]func(){
		"gs": func() {
			logger := silog.New(os.Stderr, &silog.Options{
				Level: silog.LevelDebug,
			})

			// If a secret server is configured, use it.
			var err error
			_secretStash, err = secrettest.NewClient(os.Getenv("SECRET_SERVER_URL"))
			if err != nil {
				logger.Fatalf("Could not create secret client: %v", err)
			}

			// If a browser launcher is configured, use it.
			if browserFile := os.Getenv("BROWSER_RECORDER_FILE"); browserFile != "" {
				_browserLauncher = browsertest.NewRecorder(browserFile)
			}

			// If ROBOT_INPUT is set, install a uitest.RobotView
			// instead of the normal view. This will always be interactive.
			if fixtureFile := os.Getenv("ROBOT_INPUT"); fixtureFile != "" {
				_buildView = func(_ io.Reader, stderr io.Writer, _ bool) (ui.View, error) {
					return uitest.NewRobotView(
						fixtureFile,
						&uitest.RobotViewOptions{
							OutputFile: os.Getenv("ROBOT_OUTPUT"),
							LogOutput:  stderr,
						},
					)
				}
			}

			_extraForges = append(_extraForges, &shamhub.Forge{Log: logger})
			main()
		},
		"mockedit": mockedit.Main,
		// "true" is a no-op command that always succeeds.
		"true": func() {},
		// with-term file -- cmd [args ...]
		//
		// Runs the given command inside a terminal emulator,
		// using the file to drive interactions with it.
		// See [termtest.WithTerm] for supported commands.
		"with-term": termtest.WithTerm,
	})
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

	if *_debug {
		defaultEnv["GIT_SPICE_VERBOSE"] = "true"
	}

	var shamhubCmd shamhub.Cmd

	scriptDir := filepath.Join("testdata", "script")
	if *_shardCount > 1 {
		// If we're running in sharded mode,
		// copy a subset of the test scripts
		// into a temporary directory based on the shard index.

		scripts, err := filepath.Glob(filepath.Join(scriptDir, "stack_submit_with_labels.txt"))
		require.NoError(t, err)

		shardScriptDir := t.TempDir()
		for i, script := range scripts {
			if i%*_shardCount != *_shardIndex {
				// This script does not belong to this shard.
				continue
			}

			t.Logf("Selected script: %s", script)

			bs, err := os.ReadFile(script)
			require.NoError(t, err)
			dst := filepath.Join(shardScriptDir, filepath.Base(script))
			require.NoError(t, os.WriteFile(dst, bs, 0o644))
		}

		t.Logf("Using shard script directory: %s", shardScriptDir)
		scriptDir = shardScriptDir
	}

	testscript.Run(t, testscript.Params{
		Dir:                scriptDir,
		UpdateScripts:      *_update,
		RequireUniqueNames: true,
		Setup: func(e *testscript.Env) error {
			t := e.T().(testing.TB)

			homeDir := filepath.Join(e.WorkDir, "home")
			require.NoError(t, os.Mkdir(homeDir, 0o755))
			e.Setenv("HOME", homeDir)
			e.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))

			for k, v := range defaultEnv {
				e.Setenv(k, v)
			}

			secretServer := secrettest.NewServer(t)
			t.Logf("Secret server URL: %s", secretServer.URL())
			e.Setenv("SECRET_SERVER_URL", secretServer.URL())

			shamhubCmd.Setup(t, e)
			return nil
		},
		Condition: func(cond string) (bool, error) {
			if wantVersion, ok := strings.CutPrefix(cond, "git:"); ok {
				return gittest.CondGitVersion(wantVersion)
			}

			return false, fmt.Errorf("unknown condition: %q", cond)
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

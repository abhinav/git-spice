package gittest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rogpeppe/go-internal/testscript"
	"go.abhg.dev/gs/internal/must"
)

// Fixture is a temporary directory that contains a Git repository
// built from a testscript file.
type Fixture struct {
	dir string
}

// Cleanup removes the temporary directory created by the fixture.
func (f *Fixture) Cleanup() {
	_ = os.RemoveAll(f.dir)
}

// Dir returns the directory of the fixture.
func (f *Fixture) Dir() string {
	return f.dir
}

// LoadFixtureFile loads a fixture file from the given path.
// The fixture file is expected to be testscript file
// that runs a series of git commands to set up a Git repository.
func LoadFixtureFile(path string) (_ *Fixture, err error) {
	defaultEnv := DefaultConfig().EnvMap()
	defaultEnv["EDITOR"] = "false"
	defaultEnv["GIT_AUTHOR_NAME"] = "Test"
	defaultEnv["GIT_AUTHOR_EMAIL"] = "test@example.com"
	defaultEnv["GIT_COMMITTER_NAME"] = "Test"
	defaultEnv["GIT_COMMITTER_EMAIL"] = "test@example.com"

	var (
		t          fakeT
		fixtureDir string
	)

	// Run in a separate goroutine so that FailNow and Skip
	// can call runtime.Goexit.
	done := make(chan struct{})
	go func() {
		defer close(done)

		testscript.RunT(&t, testscript.Params{
			Files: []string{path},
			// Don't delete fixtureDir when this returns.
			TestWork:           true,
			RequireUniqueNames: true,
			Setup: func(e *testscript.Env) error {
				for k, v := range defaultEnv {
					e.Setenv(k, v)
				}

				fixtureDir = e.WorkDir
				return nil
			},
			Cmds: map[string]func(*testscript.TestScript, bool, []string){
				"git": CmdGit,
				"as":  CmdAs,
				"at":  CmdAt,
			},
		})
	}()
	<-done

	if t.skipped || t.failed || t.fatal {
		return nil, fmt.Errorf("testscript failed or was skipped:\n%s", t.msgs.String())
	}

	must.NotBeBlankf(fixtureDir, "fixtureDir must not be blank")
	if _, err := os.Stat(fixtureDir); err != nil {
		must.Failf("fixtureDir must exist: %v", err)
	}

	// Persist a usable git identity into every git repository created
	// inside the fixture so that subsequent Go-driven git invocations
	// (e.g. git merge, git commit) inherit a valid identity even on
	// CI runners with no global gitconfig.
	if err := setFixtureIdentity(fixtureDir); err != nil {
		return nil, fmt.Errorf("set fixture identity: %w", err)
	}

	return &Fixture{dir: fixtureDir}, nil
}

// fixtureIdentity is appended to the local config of every git
// repository discovered inside a fixture.
const fixtureIdentity = "\n[user]\n" +
	"\tname = Test\n" +
	"\temail = test@example.com\n"

// setFixtureIdentity appends a test git identity to the local config
// of every git repository found inside dir. This includes both
// non-bare repositories (where the config lives at .git/config) and
// bare repositories (where the config lives at config).
func setFixtureIdentity(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}

		// Non-bare repo: identified by a .git directory or file
		// inside the working tree.
		if cfg := filepath.Join(path, ".git", "config"); fileExists(cfg) {
			if err := appendIdentity(cfg); err != nil {
				return err
			}
		}

		// Bare repo: identified by HEAD + config alongside refs.
		if fileExists(filepath.Join(path, "HEAD")) &&
			fileExists(filepath.Join(path, "config")) {
			if err := appendIdentity(filepath.Join(path, "config")); err != nil {
				return err
			}
		}

		return nil
	})
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func appendIdentity(configPath string) error {
	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", configPath, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(fixtureIdentity); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	return nil
}

// LoadFixtureScript loads a fixture from the testscript.
// It has access to the following commands in addition to testscript defaults:
//
//   - [CmdGit]
//   - [CmdAt]
//   - [CmdAs]
func LoadFixtureScript(script []byte) (_ *Fixture, err error) {
	// testscript.Params expects a directory with several test files in it.
	tmpDir, err := os.MkdirTemp("", "gittest-fixture-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	tmpScript := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(tmpScript, script, 0o644); err != nil {
		return nil, fmt.Errorf("write script: %w", err)
	}

	return LoadFixtureFile(tmpScript)
}

// fakeT implements testscript.T so that we can run a testscript
// without affecting the test or creating subtests.
type fakeT struct {
	fatal   bool
	failed  bool
	skipped bool
	msgs    strings.Builder
}

var _ testscript.T = (*fakeT)(nil)

// Parallel and Run are no-ops so that testscript.Run is synchronous.
func (*fakeT) Parallel()                              {}
func (f *fakeT) Run(_ string, run func(testscript.T)) { run(f) }

func (f *fakeT) FailNow() {
	f.fatal = true
	f.failed = true
	runtime.Goexit()
}

func (f *fakeT) Fatal(args ...any) {
	fmt.Fprintln(&f.msgs, fmt.Sprint(args...))
	f.FailNow()
}

func (f *fakeT) Log(args ...any) {
	fmt.Fprintln(&f.msgs, fmt.Sprint(args...))
}

func (f *fakeT) Skip(args ...any) {
	f.skipped = true
	fmt.Fprintln(&f.msgs, fmt.Sprint(args...))
	runtime.Goexit()
}

func (f *fakeT) Verbose() bool { return false }

package main

import (
	"io"
	"os"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"go.abhg.dev/gs/internal/browser"
	"go.abhg.dev/gs/internal/browser/browsertest"
	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/forge/shamhub"
	"go.abhg.dev/gs/internal/mockedit"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/secret/secrettest"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/termtest"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/uitest"
)

func TestMain(m *testing.M) {
	// Always override the secret stash with a memory stash
	// so that tests don't accidentally use the system keyring.
	_keyringStash = new(secret.MemoryStash)

	// Always override the browser launcher with a no-op launcher
	// so tests don't accidentally open a browser.
	_browserLauncher = new(browser.Noop)

	testscript.Main(m, map[string]func(){
		"gs": func() {
			cli.SetName("gs")
			logger := silog.New(os.Stderr, &silog.Options{
				Level: silog.LevelDebug,
			})

			// If a secret server is configured, use it.
			var err error
			_keyringStash, err = secrettest.NewClient(os.Getenv("SECRET_SERVER_URL"))
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

package uitest

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/ui"
)

// RunScriptsOptions defines options for RunScripts.
type RunScriptsOptions struct {
	// Size of the terminal. Defaults to 80x40.
	Rows, Cols int

	// Update specifies whether 'cmp' commands in scripts
	// should update the test file in case of mismatch.
	Update bool

	// Cmds defines additional commands to provide to the test script.
	Cmds map[string]func(*testscript.TestScript, bool, []string)
}

// RunScripts runs scripts defined in the given file or directory.
//
// It provides an "init" command that runs the provided startView function
// inside a background goroutine with access to an in-memory terminal emulator.
// Other commands provided by [SetupScript] are also available in the script.
//
// Files is a list of test script files or directories.
// For any directory specified in files, its direct children matching the name
// '*.txt' are run as scripts.
func RunScripts(
	t *testing.T,
	startView func(testing.TB, *testscript.TestScript, ui.InteractiveView),
	opts *RunScriptsOptions,
	files ...string,
) {
	require.NotEmpty(t, files, "no files provided")

	opts = cmp.Or(opts, &RunScriptsOptions{})

	type tKey struct{}
	params := testscript.Params{
		UpdateScripts: opts.Update,
		Setup: func(env *testscript.Env) error {
			env.Values[tKey{}] = env.T().(testing.TB)
			return nil
		},
	}

	for _, file := range files {
		info, err := os.Stat(files[0])
		require.NoError(t, err)
		if !info.IsDir() {
			params.Files = append(params.Files, file)
			continue
		}

		ents, err := os.ReadDir(file)
		require.NoError(t, err)
		for _, ent := range ents {
			if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".txt") {
				continue // don't recurse
			}

			params.Files = append(params.Files, filepath.Join(file, ent.Name()))
		}
	}

	// TODO: a means for waiting for the view to exit
	// so that final form can also be seen.
	setEmulator := SetupScript(&params)
	params.Cmds["init"] = func(ts *testscript.TestScript, neg bool, args []string) {
		t := ts.Value(tKey{}).(testing.TB)

		emu := NewEmulatorView(&EmulatorViewOptions{
			Rows: opts.Rows,
			Cols: opts.Cols,
			Logf: ts.Logf,
		})
		done := make(chan struct{})
		go func() {
			defer close(done)

			// TODO: set up a testing.T that will kill this goroutine
			// and mark the test as failed without panic-exploding.
			startView(t, ts, emu)
		}()
		ts.Defer(func() {
			// If the test failed, send Ctrl+C to the emulator.
			if t.Failed() {
				_ = emu.FeedKeys("\x03") // Send Ctrl+C

				ts.Logf("Try updating fixtures with the following command:\n"+
					"\tgo test -run %q -update", t.Name())
			}

			if err := emu.Close(); err != nil {
				ts.Logf("closing emulator: %v", err)
			}

			select {
			case <-done:
				// ok

			case <-time.After(3 * time.Second):
				ts.Fatalf("view did not exit in time")
			}
		})

		setEmulator(ts, emu)
	}

	if opts.Cmds != nil {
		maps.Copy(params.Cmds, opts.Cmds)
	}

	testscript.Run(t, params)
}

// SetupScript may be used from testscripts to control a fake terminal emulator.
// Install this in a testscript.Params,
// and use the returned setEmulator to provide an emulator to a test script
// with another command (e.g. "init").
//
// The source of input for the emulator should run in the background.
//
// The following commands are added to scripts:
//
//	clear
//		Ignore current screen contents when awaiting text.
//	await [txt]
//		Wait for the given text to be visible on screen.
//		If [txt] is absent, wait until the contents of the screen
//		change compared to the last 'snapshot' call.
//	snapshot
//		Print a copy of the screen to the script's stdout.
//	feed [-r N] txt
//		Post the given text to the command's stdin.
//		If -count is given, the input is repeated N times.
func SetupScript(params *testscript.Params) (setEmulator func(*testscript.TestScript, Emulator)) {
	type stateKey struct{}
	type stateValue struct{ V *scriptState }

	getState := func(ts *testscript.TestScript) *scriptState {
		container := ts.Value(stateKey{}).(*stateValue)
		if container.V == nil {
			ts.Fatalf("setEmulator not called: no state found")
		}
		return container.V
	}

	oldSetup := params.Setup
	params.Setup = func(env *testscript.Env) error {
		if oldSetup != nil {
			if err := oldSetup(env); err != nil {
				return err
			}
		}

		env.Values[stateKey{}] = new(stateValue)
		return nil
	}

	if params.Cmds == nil {
		params.Cmds = make(map[string]func(ts *testscript.TestScript, neg bool, args []string))
	}

	// clear
	params.Cmds["clear"] = func(ts *testscript.TestScript, neg bool, args []string) {
		if neg {
			ts.Fatalf("usage: clear")
		}

		getState(ts).Clear()
	}

	// await [txt]
	params.Cmds["await"] = func(ts *testscript.TestScript, neg bool, args []string) {
		if neg {
			ts.Fatalf("usage: await [txt]")
		}

		state := getState(ts)
		want := strings.Join(args, " ")
		if err := state.Await(want); err != nil {
			ts.Fatalf("await %q: %v", want, err)
		}
	}

	// snapshot
	params.Cmds["snapshot"] = func(ts *testscript.TestScript, neg bool, args []string) {
		if neg || len(args) != 0 {
			ts.Fatalf("usage: snapshot")
		}

		state := getState(ts)
		state.Snapshot("")
	}

	// TODO: up/down/enter?
	// feed [-r n] txt
	params.Cmds["feed"] = func(ts *testscript.TestScript, neg bool, args []string) {
		flag := flag.NewFlagSet("feed", flag.ContinueOnError)
		flag.Usage = func() {
			ts.Logf("usage: feed [-r n] txt")
		}
		if neg {
			flag.Usage()
			ts.Fatalf("incorrect usage")
		}

		repeat := flag.Int("r", 1, "repetitions of the input")
		if err := flag.Parse(args); err != nil {
			ts.Fatalf("feed: %v", err)
		}

		args = flag.Args()
		if len(args) == 0 {
			flag.Usage()
			ts.Fatalf("incorrect usage")
		}

		state := getState(ts)
		keys := strings.Repeat(strings.Join(args, ""), *repeat)
		if err := state.Feed(keys); err != nil {
			ts.Fatalf("feed %q: %v", keys, err)
		}
	}

	return func(ts *testscript.TestScript, emu Emulator) {
		ts.Value(stateKey{}).(*stateValue).V = newScriptState(emu, &scriptStateOptions{
			Logf:   ts.Logf,
			Output: ts.Stdout,
		})
	}
}

// Emulator is a terminal emulator that receives input
// and allows querying the terminal state.
type Emulator interface {
	FeedKeys(string) error
	Rows() []string
}

var _ Emulator = (*EmulatorView)(nil)

// ScriptOptions are options for running a script against
type ScriptOptions struct {
	// Logf is a function that logs messages.
	//
	// No logging is done if Logf is nil.
	Logf func(string, ...any)

	// Output is the writer to which snapshots are written.
	Output io.Writer
}

// Script runs a UI script defining terminal interactions
// against a terminal emulator.
//
// UI scripts take the form of newline-separated commands.
//
//	await Enter a name
//	feed Foo\r
//	snapshot
//
// The following commands are supported.
//
//   - await [txt]:
//     Wait up to 1 second for the given text to become visible on the screen.
//     If [txt] is absent, wait until contents of the screen change
//     compared to the last captured snapshot or last await empty.
//   - clear:
//     Ignore current screen contents when awaiting text.
//   - snapshot [name]:
//     Take a picture of the screen as it is right now, and print it to Output.
//     If name is provided, the output will include that as a header.
//   - feed txt:
//     Feed the given string into the terminal.
//     Go string-style escape codes are permitted without quotes.
//     Examples: \r, \x1b[B
//
// Snapshots are written to [ScriptOptions.Output] if provided.
//
// New scripts should prefer using SetupScript for this purpose.
func Script(emu Emulator, script []byte, opts *ScriptOptions) error {
	opts = cmp.Or(opts, &ScriptOptions{})

	stateOpts := &scriptStateOptions{
		Logf: opts.Logf,
	}
	if opts.Output != nil {
		stateOpts.Output = func() io.Writer {
			return opts.Output
		}
	}

	state := newScriptState(emu, stateOpts)
	scan := bufio.NewScanner(bytes.NewReader(script))
	for scan.Scan() {
		line := bytes.TrimSpace(scan.Bytes())
		if len(line) == 0 {
			continue
		}

		if line[0] == '#' {
			continue // ignore comments
		}

		cmd, rest, _ := strings.Cut(string(line), " ")
		switch cmd {
		case "clear":
			state.Clear()

		case "await":
			if err := state.Await(rest); err != nil {
				return fmt.Errorf("await: %w", err)
			}

		case "snapshot":
			state.Snapshot(rest)

		case "feed":
			s := strings.ReplaceAll(rest, `"`, `\"`)
			s = `"` + s + `"`
			keys, err := strconv.Unquote(s)
			if err != nil {
				return fmt.Errorf("cannot unquote: %v", rest)
			}

			if err := state.Feed(keys); err != nil {
				return fmt.Errorf("feed: %w", err)
			}
		}
	}

	return nil
}

type scriptStateOptions struct {
	Logf   func(string, ...any)
	Output func() io.Writer
}

type scriptState struct {
	emu              Emulator
	lastSnapshot     []string
	awaitStripPrefix []string

	logf   func(string, ...any)
	output func() io.Writer
}

func newScriptState(emu Emulator, opts *scriptStateOptions) *scriptState {
	opts = cmp.Or(opts, &scriptStateOptions{})
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	output := opts.Output
	if output == nil {
		output = func() io.Writer { return io.Discard }
	}

	return &scriptState{
		emu:    emu,
		logf:   logf,
		output: output,
	}
}

func (s *scriptState) Clear() {
	s.awaitStripPrefix = s.emu.Rows()
}

func (s *scriptState) Await(want string) error {
	timeout := 3 * time.Second
	start := time.Now()

	var match func([]string) bool
	switch {
	case len(want) > 0:
		match = func(snap []string) bool {
			// Strip prefix if "clear" was called.
			if len(s.awaitStripPrefix) > 0 && len(snap) >= len(s.awaitStripPrefix) {
				for i := 0; i < len(s.awaitStripPrefix); i++ {
					if snap[i] != s.awaitStripPrefix[i] {
						s.awaitStripPrefix = nil
						break
					}
				}

				if len(s.awaitStripPrefix) > 0 {
					snap = snap[len(s.awaitStripPrefix):]
				}
			}

			for _, line := range snap {
				if strings.Contains(line, want) {
					return true
				}
			}
			return false
		}
	case len(s.lastSnapshot) > 0:
		want := s.lastSnapshot
		s.lastSnapshot = nil
		match = func(snap []string) bool {
			return !slices.Equal(snap, want)
		}

	default:
		return errors.New("argument is required if no snapshots were captured")
	}

	var (
		last    []string
		matched bool
	)
	for time.Since(start) < timeout {
		last = s.emu.Rows()
		if match(last) {
			matched = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !matched {
		s.logf("### screen ###")
		for _, line := range last {
			s.logf("%s", line)
		}

		if len(want) > 0 {
			return fmt.Errorf("%q not found", want)
		}
		return errors.New("screen did not change")
	}

	// If 'await' was given without an argument,
	// save the match as the last snapshot.
	if len(want) == 0 {
		s.lastSnapshot = last
	}

	return nil
}

func (s *scriptState) Snapshot(title string) {
	output := s.output()
	s.lastSnapshot = s.emu.Rows()
	if len(title) > 0 {
		fmt.Fprintf(output, "### %s ###\n", title)
	}
	for _, line := range s.lastSnapshot {
		fmt.Fprintln(output, line)
	}
}

var _keyReplacements = map[string]string{
	"<Up>":    "\x1b[A",
	"<Down>":  "\x1b[B",
	"<Right>": "\x1b[C",

	"<Enter>": "\r",
	"<Space>": " ",
	"<Tab>":   "\t",

	"<Bs>":        "\x08",
	"<Backspace>": "\x08",
}

var _keyReplacer *strings.Replacer

func init() {
	var repl []string
	for k, v := range _keyReplacements {
		repl = append(repl, k, v)
		repl = append(repl, strings.ToUpper(k), v)
		repl = append(repl, strings.ToLower(k), v)
	}

	_keyReplacer = strings.NewReplacer(repl...)
}

func (s *scriptState) Feed(keys string) error {
	keys = _keyReplacer.Replace(keys)
	if err := s.emu.FeedKeys(keys); err != nil {
		return fmt.Errorf("feed keys %q: %w", keys, err)
	}
	return nil
}

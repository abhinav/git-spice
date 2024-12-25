package uitest

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
)

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

func (s *scriptState) Feed(keys string) error {
	if err := s.emu.FeedKeys(keys); err != nil {
		return fmt.Errorf("feed keys %q: %w", keys, err)
	}
	return nil
}

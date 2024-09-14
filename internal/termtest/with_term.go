// Package termtest provides utilities for testing terminal-based programs.
package termtest

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty/v2"
	"github.com/vito/midterm"
)

// WithTerm is an entry point for a command line program "with-term".
// Its usage is as follows:
//
//	with-term [options] script -- cmd [args ...]
//
// It runs cmd with the given arguments inside a terminal emulator,
// using the script file to drive interactions with it.
//
// The file contains a series of newline delimited commands.
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
//     Take a picture of the screen as it is right now, and print it to stdout.
//     If name is provided, the output will include that as a header.
//   - feed txt:
//     Feed the given string into the terminal.
//     Go string-style escape codes are permitted without quotes.
//     Examples: \r, \x1b[B
//
// The following options may be provided before the script file.
//
//   - -cols int: terminal width (default 80)
//   - -rows int: terminal height (default 40)
//   - -fixed: don't auto-grow the terminal as output increases
//   - -final <name>:
//     print a final snapshot on exit to stdout with the given name.
func WithTerm() (exitCode int) {
	cols := flag.Int("cols", 80, "terminal width")
	rows := flag.Int("rows", 40, "terminal height")
	fixed := flag.Bool("fixed", false,
		"don't automatically resize the terminal as output increases "+
			"(useful for testing scrolling behavior)")
	finalSnapshot := flag.String("final", "",
		"capture a snapshot on exit with the given name")

	log.SetFlags(0)
	log.SetOutput(os.Stderr)
	log.SetPrefix("term: ")
	flag.Parse()

	args := flag.Args()
	if len(args) < 2 {
		log.Println("usage: with-term file -- cmd [args ...]")
		return 1
	}

	instructions, args := args[0], args[1:]
	instructionFile, err := os.Open(instructions)
	if err != nil {
		log.Printf("cannot open instructions: %v", err)
		return 1
	}
	defer func() {
		if err := instructionFile.Close(); err != nil {
			log.Printf("cannot close instructions: %v", err)
			exitCode = 1
		}
	}()

	if args[0] == "--" {
		args = args[1:]
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "TERM=screen")

	size := &pty.Winsize{
		Rows: uint16(*rows),
		Cols: uint16(*cols),
	}
	f, err := pty.StartWithSize(cmd, size)
	if err != nil {
		log.Printf("cannot open pty: %v", err)
		return 1
	}

	emu := newVT100Emulator(f, cmd,
		int(size.Rows),
		int(size.Cols),
		!*fixed,
		log.Printf,
	)
	defer func() {
		if err := emu.Close(); err != nil {
			log.Printf("%v: %v", cmd, err)
			exitCode = 1
		}

		if *finalSnapshot != "" {
			if len(*finalSnapshot) > 0 {
				fmt.Printf("### %s ###\n", *finalSnapshot)
			}
			for _, line := range emu.Snapshot() {
				fmt.Println(line)
			}
		}
	}()

	var (
		// lastSnapshot is the last snapshot taken.
		lastSnapshot []string

		// lastMatchPrefix holds the contents of the screen
		// up to and including the last 'await txt' match.
		awaitStripPrefix []string
	)
	scan := bufio.NewScanner(instructionFile)
	for scan.Scan() {
		line := bytes.TrimSpace(scan.Bytes())
		if len(line) == 0 {
			continue
		}

		cmd, rest, _ := strings.Cut(string(line), " ")
		switch cmd {
		case "clear":
			awaitStripPrefix = emu.Snapshot()

		case "await":
			timeout := 3 * time.Second
			start := time.Now()

			var match func([]string) bool
			switch {
			case len(rest) > 0:
				want := rest
				match = func(snap []string) bool {
					// Strip prefix if "clear" was called.
					if len(awaitStripPrefix) > 0 && len(snap) >= len(awaitStripPrefix) {
						for i := 0; i < len(awaitStripPrefix); i++ {
							if snap[i] != awaitStripPrefix[i] {
								awaitStripPrefix = nil
								break
							}
						}

						if len(awaitStripPrefix) > 0 {
							snap = snap[len(awaitStripPrefix):]
						}
					}

					for _, line := range snap {
						if strings.Contains(line, want) {
							return true
						}
					}
					return false
				}
			case len(lastSnapshot) > 0:
				want := lastSnapshot
				lastSnapshot = nil
				match = func(snap []string) bool {
					return !slices.Equal(snap, want)
				}

			default:
				log.Printf("await: argument is required if no snapshots were captured")
				continue
			}

			var (
				last    []string
				matched bool
			)
			for time.Since(start) < timeout {
				last = emu.Snapshot()
				if match(last) {
					matched = true
					break
				}
				time.Sleep(50 * time.Millisecond)
			}

			if !matched {
				if len(rest) > 0 {
					log.Printf("await: %q not found", rest)
					exitCode = 1
				} else {
					log.Printf("await: screen did not change")
					exitCode = 1
				}

				log.Printf("### screen ###")
				for _, line := range last {
					log.Printf("%s", line)
				}
			}

			// If 'await' was given without an argument,
			// save the match as the last snapshot.
			if len(rest) == 0 {
				lastSnapshot = last
			}

		case "snapshot":
			lastSnapshot = emu.Snapshot()
			if len(rest) > 0 {
				fmt.Printf("### %s ###\n", rest)
			}
			for _, line := range lastSnapshot {
				fmt.Println(line)
			}

		case "feed":
			s := strings.ReplaceAll(rest, `"`, `\"`)
			s = `"` + s + `"`
			keys, err := strconv.Unquote(s)
			if err != nil {
				log.Printf("cannot unquote: %v", rest)
				return 1
			}

			if err := emu.FeedKeys(keys); err != nil {
				log.Printf("error feeding keys: %v", err)
			}
		}
	}

	return exitCode
}

type terminalEmulator struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	pty  pty.Pty
	logf func(string, ...any)

	term *midterm.Terminal
}

func newVT100Emulator(
	f pty.Pty,
	cmd *exec.Cmd,
	rows, cols int,
	autoResize bool,
	logf func(string, ...any),
) *terminalEmulator {
	if logf == nil {
		logf = log.Printf
	}
	term := midterm.NewTerminal(rows, cols)
	term.AutoResizeX = autoResize
	term.AutoResizeY = autoResize
	m := terminalEmulator{
		pty:  f,
		cmd:  cmd,
		term: term,
		logf: logf,
	}
	go m.Start()
	return &m
}

func (m *terminalEmulator) Start() {
	var buffer [1024]byte
loop:
	for {
		n, err := m.pty.Read(buffer[0:])
		if n > 0 {
			m.mu.Lock()
			_, writeErr := m.term.Write(buffer[:n])
			m.mu.Unlock()
			if writeErr != nil {
				m.logf("decode error: %v", writeErr)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				m.logf("read error: %v", err)
			}
			break loop
		}
	}
}

func (m *terminalEmulator) Close() error {
	_, writeErr := m.pty.Write([]byte{4}) // EOT
	if writeErr != nil {
		writeErr = fmt.Errorf("send EOT: %w", writeErr)
	}

	waitErrc := make(chan error, 1)
	go func() {
		err := m.cmd.Wait()
		if err != nil {
			err = fmt.Errorf("command %v: %w", m.cmd, err)
		}
		waitErrc <- err
	}()

	var waitErr error
	select {
	case waitErr = <-waitErrc:
		// ok
	case <-time.After(10 * time.Second):
		waitErr = fmt.Errorf("timeout waiting for %v", m.cmd)
		_ = m.cmd.Process.Kill()
	}

	errs := []error{waitErr, writeErr}
	if runtime.GOOS != "windows" {
		// On Windows, pty.Close seems to freeze right now.
		errs = append(errs, m.pty.Close())
	}
	return errors.Join(errs...)
}

func (m *terminalEmulator) FeedKeys(s string) error {
	_, err := io.WriteString(m.pty, s)
	return err
}

func (m *terminalEmulator) Snapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return Rows(m.term.Screen)
}

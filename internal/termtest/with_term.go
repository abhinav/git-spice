// Package termtest provides utilities for testing terminal-based programs.
package termtest

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
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
//	with-term script -- cmd [args ...]
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
//     compared to the last captured snapshot.
//   - snapshot [name]:
//     Take a picture of the screen as it is right now, and print it to stdout.
//     If name is provided, the output will include that as a header.
//   - feed txt:
//     Feed the given string into the terminal.
//     Go string-style escape codes are permitted without quotes.
//     Examples: \r, \x1b[B
func WithTerm() (exitCode int) {
	log.SetFlags(0)

	args := os.Args[1:]
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
	cmd.Stderr = os.Stderr

	size := &pty.Winsize{
		Rows: 40,
		Cols: 70,
	}
	f, err := pty.StartWithSize(cmd, size)
	if err != nil {
		log.Printf("cannot open pty: %v", err)
		return 1
	}

	emu := newVT100Emulator(f, cmd, int(size.Rows), int(size.Cols), log.Printf)
	defer func() {
		if err := emu.Close(); err != nil {
			log.Printf("%v: %v", cmd, err)
			exitCode = 1
		}
	}()

	var lastSnapshot []byte
	scan := bufio.NewScanner(instructionFile)
	for scan.Scan() {
		line := bytes.TrimSpace(scan.Bytes())
		if len(line) == 0 {
			continue
		}

		cmd, rest, _ := strings.Cut(string(line), " ")
		switch cmd {
		case "await":
			timeout := time.Second
			start := time.Now()

			var match func([]byte) bool
			switch {
			case len(rest) > 0:
				want := []byte(rest)
				match = func(snap []byte) bool {
					return bytes.Contains(snap, want)
				}
			case len(lastSnapshot) > 0:
				want := lastSnapshot
				lastSnapshot = nil
				match = func(snap []byte) bool {
					return !bytes.Equal(snap, want)
				}

			default:
				log.Printf("await: argument is required if no snapshots were captured")
				continue
			}

			for time.Since(start) < timeout {
				if match(emu.Snapshot()) {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}

		case "snapshot":
			lastSnapshot = emu.Snapshot()
			if len(rest) > 0 {
				fmt.Printf("### %s ###\n", rest)
			}
			if _, err := os.Stdout.Write(lastSnapshot); err != nil {
				log.Printf("error writing to stdout: %v", err)
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

	return 0
}

type terminalEmulator struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	pty  *os.File
	logf func(string, ...any)

	term *midterm.Terminal
}

func newVT100Emulator(
	f *os.File,
	cmd *exec.Cmd,
	rows, cols int,
	logf func(string, ...any),
) *terminalEmulator {
	if logf == nil {
		logf = log.Printf
	}
	term := midterm.NewTerminal(rows, cols)
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
			if !errors.Is(err, io.EOF) {
				m.logf("read error: %v", err)
			}
			break loop
		}
	}
}

func (m *terminalEmulator) Close() error {
	_, err := m.pty.Write([]byte{4}) // EOT
	return errors.Join(
		err,
		m.cmd.Wait(),
		m.pty.Close(),
	)
}

func (m *terminalEmulator) FeedKeys(s string) error {
	_, err := io.WriteString(m.pty, s)
	_ = m.pty.Sync()
	return err
}

func (m *terminalEmulator) Snapshot() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()

	var buff bytes.Buffer
	for _, row := range m.term.Content {
		rowstr := strings.TrimRight(string(row), " \t\n")
		buff.WriteString(rowstr)
		buff.WriteRune('\n')
	}

	return append(bytes.TrimRight(buff.Bytes(), "\n"), '\n')
}

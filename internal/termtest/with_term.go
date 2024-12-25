// Package termtest provides utilities for testing terminal-based programs.
package termtest

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"go.abhg.dev/gs/internal/ui/uitest"
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
// See [uitest.Script] for more details on the script format.
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
	flag.Parse()

	args := flag.Args()
	if len(args) < 2 {
		log.Println("usage: with-term file -- cmd [args ...]")
		return 1
	}

	scriptPath, args := args[0], args[1:]
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		log.Printf("cannot open instructions: %v", err)
		return 1
	}

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
		*fixed,
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
			for _, line := range emu.Rows() {
				fmt.Println(line)
			}
		}
	}()

	err = uitest.Script(emu, script, &uitest.ScriptOptions{
		Logf:   log.Printf,
		Output: os.Stdout,
	})
	if err != nil {
		log.Printf("script error: %v", err)
		return 1
	}

	return exitCode
}

type terminalEmulator struct {
	cmd  *exec.Cmd
	pty  *os.File
	logf func(string, ...any)
	emu  *uitest.EmulatorView
}

func newVT100Emulator(
	f *os.File,
	cmd *exec.Cmd,
	rows, cols int,
	noAutoResize bool,
	logf func(string, ...any),
) *terminalEmulator {
	if logf == nil {
		logf = log.Printf
	}
	m := terminalEmulator{
		pty: f,
		cmd: cmd,
		emu: uitest.NewEmulatorView(&uitest.EmulatorViewOptions{
			Rows:         rows,
			Cols:         cols,
			NoAutoResize: noAutoResize,
		}),
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
			_, writeErr := m.emu.Write(buffer[:n])
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

	waitErrc := make(chan error, 1)
	go func() {
		waitErrc <- m.cmd.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-waitErrc:
		// ok
	case <-time.After(3 * time.Second):
		waitErr = fmt.Errorf("timeout waiting for %v", m.cmd)
		_ = m.cmd.Process.Kill()
	}

	closeErr := m.pty.Close()

	return errors.Join(waitErr, closeErr, writeErr)
}

func (m *terminalEmulator) FeedKeys(s string) error {
	_, err := io.WriteString(m.pty, s)
	_ = m.pty.Sync()
	return err
}

func (m *terminalEmulator) Rows() []string {
	return m.emu.Rows()
}

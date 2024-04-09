package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty/v2"
	"github.com/rogpeppe/go-internal/testscript"
	"github.com/vito/midterm"
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
		// The file contains a series of newline delimited commands.
		//
		//  - await [txt]:
		//    Wait up to 1 second for the given text to become visible
		//    on the screen.
		//    If [txt] is absent, wait until contents of the screen
		//    have changed since the last captured snapshot.
		//  - feed txt:
		//    Feed the given string into the terminal.
		//    Go string-style escape codes are permitted.
		//    Examples:
		//      Enter	   \r
		//      Down arrow \x1b[B
		//  - snapshot [name]:
		//    Take a picture of the screen as it is right now,
		//    and print it to stdout.
		//    If name is provided,
		//    the output will include that as a header.
		"with-term": func() (exitCode int) {
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
			defer instructionFile.Close()

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
					if len(rest) > 0 {
						want := []byte(rest)
						match = func(snap []byte) bool {
							return bytes.Contains(snap, want)
						}
					} else if len(lastSnapshot) > 0 {
						want := lastSnapshot
						lastSnapshot = nil
						match = func(snap []byte) bool {
							return !bytes.Equal(snap, want)
						}

					} else {
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
		},
	})
}

func TestScript(t *testing.T) {
	defaultGitConfig := map[string]string{
		"init.defaultBranch": "main",
	}

	testscript.Run(t, testscript.Params{
		Dir:                filepath.Join("testdata", "script"),
		UpdateScripts:      *_update,
		RequireUniqueNames: true,
		Setup: func(e *testscript.Env) error {
			// We can set Git configuration values by setting
			// GIT_CONFIG_KEY_<n>, GIT_CONFIG_VALUE_<n> and GIT_CONFIG_COUNT.
			var numCfg int
			for k, v := range defaultGitConfig {
				n := strconv.Itoa(numCfg)
				e.Setenv("GIT_CONFIG_KEY_"+n, k)
				e.Setenv("GIT_CONFIG_VALUE_"+n, v)
				numCfg++
			}
			e.Setenv("GIT_CONFIG_COUNT", strconv.Itoa(numCfg))

			return nil
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"git": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg {
					ts.Fatalf("usage: git <args>")
				}
				ts.Check(ts.Exec("git", args...))
			},
			"as": func(ts *testscript.TestScript, neg bool, args []string) {
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
			},
			"at": func(ts *testscript.TestScript, neg bool, args []string) {
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
			},
		},
	})
}

type terminalEmulator struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	pty  *os.File
	logf func(string, ...any)

	term *midterm.Terminal
}

func newVT100Emulator(f *os.File, cmd *exec.Cmd, rows, cols int, logf func(string, ...any)) *terminalEmulator {
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
	m.pty.Write([]byte{4}) // EOT
	err := m.cmd.Wait()
	_ = m.pty.Close()
	return err
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

package ui_test

import (
	"bytes"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hexops/autogold/v2"
	"github.com/stretchr/testify/require"
	"github.com/vito/midterm"
	"go.abhg.dev/gs/internal/ui"
)

func TestOutputWriter_TeaProgram_printsLines(t *testing.T) {
	var raw lockedBuffer
	output := ui.NewOutputWriter(&raw)
	defer func() {
		require.NoError(t, output.Close())
	}()
	model := new(outputWriterProgramModel)

	errc := make(chan error, 1)
	go func() {
		errc <- ui.RunModel(model, &ui.RunOptions{
			Input:          bytes.NewReader(nil),
			Output:         output,
			Width:          40,
			Height:         8,
			TERM:           "xterm-256color",
			WithoutSignals: true,
		})
	}()

	var rows []string
	require.Eventually(t, func() bool {
		rows = renderedRowsFrom(t, raw.Bytes())
		return slices.Contains(rows, "model frame")
	}, time.Second, 10*time.Millisecond)
	autogold.Expect([]string{
		"model frame",
	}).Equal(t, rows)

	_, err := output.Write([]byte("log line\n"))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		rows = renderedRowsFrom(t, raw.Bytes())
		return slices.Contains(rows, "log line") &&
			slices.Contains(rows, "model frame")
	}, time.Second, 10*time.Millisecond)
	autogold.Expect([]string{
		"log line",
		"model frame",
	}).Equal(t, rows)

	model.Stop()
	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for program to stop")
	}
}

func TestOutputWriter_TeaProgram_writeReturnsWhileModelBlocked(t *testing.T) {
	var raw lockedBuffer
	output := ui.NewOutputWriter(&raw)
	defer func() {
		require.NoError(t, output.Close())
	}()
	release := make(chan struct{})
	model := &outputWriterBlockedModel{
		blocked: make(chan struct{}),
		release: release,
	}

	errc := make(chan error, 1)
	go func() {
		errc <- ui.RunModel(model, &ui.RunOptions{
			Input:          bytes.NewReader(nil),
			Output:         output,
			Width:          40,
			Height:         8,
			TERM:           "xterm-256color",
			WithoutSignals: true,
		})
	}()

	// The model must enter its blocking Update before the write.
	// Otherwise, the test only proves that output can be delivered to an idle
	// Bubble Tea program.
	require.Eventually(t, func() bool {
		return slices.Contains(
			renderedRowsFrom(t, raw.Bytes()),
			"blocked model",
		)
	}, time.Second, 10*time.Millisecond)

	select {
	case <-model.blocked:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for model to block")
	}

	// This write is the deadlock boundary.
	// A synchronous Program.Send needs the Bubble Tea update loop to receive
	// the print message, but the update loop is intentionally blocked above.
	writec := make(chan error, 1)
	go func() {
		_, err := output.Write([]byte("log line\n"))
		writec <- err
	}()

	select {
	case err := <-writec:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for output write")
	}

	close(release)

	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for program to stop")
	}
}

type outputWriterProgramModel struct {
	done atomic.Bool
}

type outputWriterProgramTickMsg struct{}

// Init schedules the first tick without blocking the initial render.
func (m *outputWriterProgramModel) Init() tea.Cmd {
	return tea.Tick(10*time.Millisecond, func(time.Time) tea.Msg {
		return outputWriterProgramTickMsg{}
	})
}

// Update observes Stop through tick messages and quits once Stop has run.
func (m *outputWriterProgramModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(outputWriterProgramTickMsg); !ok {
		return m, nil
	}

	if m.done.Load() {
		return m, tea.Quit
	}
	return m, tea.Tick(10*time.Millisecond, func(time.Time) tea.Msg {
		return outputWriterProgramTickMsg{}
	})
}

func (m *outputWriterProgramModel) View() tea.View {
	return tea.NewView("model frame")
}

// Stop asks the next tick message to stop the Bubble Tea program.
func (m *outputWriterProgramModel) Stop() {
	m.done.Store(true)
}

// outputWriterBlockedModel simulates a Bubble Tea program that is alive but
// unable to receive print messages through its update loop.
//
// This matches the branch-submit failure shape: the form waits for a background
// result while that background path tries to log through the active program.
type outputWriterBlockedModel struct {
	blocked chan struct{}   // closed after Update enters the blocked section
	release <-chan struct{} // unblocks Update so the test can shut down

	once sync.Once
}

type outputWriterBlockedMsg struct{}

func (m *outputWriterBlockedModel) Init() tea.Cmd {
	return tea.Tick(10*time.Millisecond, func(time.Time) tea.Msg {
		return outputWriterBlockedMsg{}
	})
}

func (m *outputWriterBlockedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(outputWriterBlockedMsg); !ok {
		return m, nil
	}

	// Keep Update blocked until the test proves OutputWriter.Write returned.
	m.once.Do(func() {
		close(m.blocked)
	})
	<-m.release
	return m, tea.Quit
}

func (m *outputWriterBlockedModel) View() tea.View {
	return tea.NewView("blocked model")
}

// lockedBuffer protects terminal output while the program is still rendering.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends terminal output from Bubble Tea and OutputWriter.
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

// Bytes returns a stable snapshot of all terminal output written so far.
func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()

	return slices.Clone(b.buf.Bytes())
}

// renderedRowsFrom replays terminal bytes into midterm rows.
func renderedRowsFrom(t *testing.T, raw []byte) []string {
	t.Helper()

	term := midterm.NewTerminal(8, 40)
	term.Raw = true
	_, err := term.Write(raw)
	require.NoError(t, err)

	rows := make([]string, len(term.Content))
	for idx, row := range term.Content {
		rows[idx] = string(trimRightWS(row))
	}
	return slices.Clip(trimRightEmpty(rows))
}

func trimRightEmpty(rows []string) []string {
	for idx, row := range slices.Backward(rows) {
		if row != "" {
			return rows[:idx+1]
		}
	}
	return nil
}

func trimRightWS(rs []rune) []rune {
	for idx, v := range slices.Backward(rs) {
		switch v {
		case 0, ' ', '\t', '\n':
		default:
			rs = rs[:idx+1]
			if j := slices.Index(rs, 0); j >= 0 {
				rs = slices.Clone(rs)
				for k := j; k < len(rs); k++ {
					if rs[k] == 0 {
						rs[k] = ' '
					}
				}
			}
			return rs
		}
	}
	return nil
}

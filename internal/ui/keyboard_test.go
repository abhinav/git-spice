package ui_test

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/ui"
)

// A form temporarily asks the terminal to report keys
// using the Kitty keyboard protocol,
// which can change the bytes sent for a key press.
// In #1465, after a confirmation form exited,
// the terminal emulator still sent ESC[1;1A for Up instead of ESC[A,
// so the shell no longer recognized the arrow key.
// Bubble Tea cleared the protocol flags on exit
// without restoring the prior keyboard state;
// popping the keyboard stack restored normal shell input.
//
// https://github.com/abhinav/git-spice/issues/1465.
func TestForm_Run_restoresKeyboardProtocol(t *testing.T) {
	var output bytes.Buffer
	form := ui.NewForm(ui.NewConfirm().WithTitle("Delete branch?"))
	require.NoError(t, form.Run(&ui.FormRunOptions{
		Input:          strings.NewReader("\r"),
		Output:         &output,
		Theme:          ui.DefaultThemeDark(),
		Width:          80,
		Height:         24,
		TERM:           "xterm-256color",
		WithoutSignals: true,
	}))
	require.Contains(t, output.String(), "Delete branch?")

	// The terminal keeps a stack of keyboard modes
	// so a program can use its own mode,
	// then restore its caller's mode when it exits.
	// These output commands control that stack; CSI means ESC followed by '[':
	//
	//   CSI > flags u        Push a mode with the requested flags.
	//   CSI = flags ; mode u Change flags in the current mode without pushing.
	//   CSI < count u        Pop modes, restoring the mode underneath.
	//
	// Flags are a bitmask selecting key-reporting features.
	// Clearing them changes the current mode; it does not remove that mode.
	// In the reported failure,
	// clearing flags left the changed arrow-key encoding active until the mode was popped.
	//
	// Track modes pushed by this form: depth zero is the caller's mode.
	// Flag changes require positive depth, pops must not reach below zero,
	// and the form must leave depth at zero when it returns to the shell.
	// Inspect output commands
	// because the screen emulator does not implement keyboard modes;
	// this checks stack use, not the terminal emulator's key encoding itself.
	// See https://sw.kovidgoyal.net/kitty/keyboard-protocol/#progressive-enhancement.
	keyboard := regexp.MustCompile("\x1b" + `\[([=<>])([0-9;]*)u`)
	commands := keyboard.FindAllStringSubmatch(output.String(), -1)
	require.NotEmpty(t, commands, "prompt did not exercise keyboard protocol")
	var depth, pushes int
	for _, command := range commands {
		switch command[1] {
		case ">":
			depth++
			pushes++
		case "<":
			count := 1
			if command[2] != "" {
				var err error
				count, err = strconv.Atoi(command[2])
				require.NoError(t, err)
			}
			depth -= count
			require.GreaterOrEqual(t, depth, 0, "popped caller's keyboard state: %q", command[0])
		case "=":
			assert.Positive(t, depth, "overwrote caller's keyboard state without saving it: %q", command[0])
		}
	}
	assert.Positive(t, pushes, "prompt must save the caller's keyboard state")
	assert.Zero(t, depth, "prompt left keyboard state on the stack")
}

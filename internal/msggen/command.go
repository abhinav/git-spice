package msggen

import (
	"context"
	"errors"
	"fmt"
	"os"

	"go.abhg.dev/gs/internal/scriptrun"
)

// ScriptRunner executes a configured message command with its process streams.
type ScriptRunner interface {
	Run(context.Context, *scriptrun.RunRequest) (*scriptrun.RunResult, error)
}

var _ ScriptRunner = (*scriptrun.Runner)(nil)

// ErrNoCommand reports that filling needs an external message command.
var ErrNoCommand = errors.New("message generation requires spice.message.fill.command")

func runMessageCommand(ctx context.Context, runner ScriptRunner, req *scriptrun.RunRequest) error {
	if req.Script == "" {
		return ErrNoCommand
	}
	req.Stdin, req.Stdout, req.Stderr = os.Stdin, os.Stdout, os.Stderr
	result, err := runner.Run(ctx, req)
	if err != nil {
		return fmt.Errorf("run message command: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("message command exited with status %d", result.ExitCode)
	}
	return nil
}

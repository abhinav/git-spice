// Package msggen generates commit and CR messages with user-configured commands.
// Commands edit a message file and inherit all three standard streams.
package msggen

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/scriptrun"
)

// Handler generates messages for committed ranges with a configured command.
type Handler struct {
	// Fill enables automatic message filling.
	Fill bool

	// Command edits message files in place. Empty selects built-in filling
	// where the invoking workflow supports it.
	Command string

	// Dir is the worktree root. Empty uses the current directory.
	Dir string

	Runner ScriptRunner // required
}

// Enabled reports whether automatic filling was selected.
func (h *Handler) Enabled() bool { return h.Fill }

// Configured reports whether an external fill command is configured.
func (h *Handler) Configured() bool { return h.Command != "" }

// RangeRequest describes the committed range selected by squash or submit.
type RangeRequest struct {
	Operation OperationKind // required

	// Base and Head are the commit endpoints of the selected range.
	Base, Head git.Hash // required
}

// Generate supplies an empty message file and returns the message the command wrote.
func (h *Handler) Generate(ctx context.Context, req *RangeRequest) (string, error) {
	f, err := os.CreateTemp("", "gs-message-*")
	if err != nil {
		return "", fmt.Errorf("create message file: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close message file: %w", err)
	}

	if err := runMessageCommand(ctx, h.Runner, &scriptrun.RunRequest{
		Script: h.Command,
		Dir:    h.Dir,
		Args:   []string{f.Name(), req.Operation.String(), req.Base.String(), req.Head.String()},
	}); err != nil {
		return "", err
	}

	data, err := os.ReadFile(f.Name())
	if err != nil {
		return "", fmt.Errorf("read generated message: %w", err)
	}
	message := string(data)
	subject, _, _ := strings.Cut(message, "\n")
	if strings.TrimSpace(subject) == "" {
		return "", errors.New("generated message must start with a non-empty subject")
	}
	return message, nil
}

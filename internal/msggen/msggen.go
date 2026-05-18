// Package msggen provides support for running external scripts
// to generate or update commit and PR messages.
//
// Scripts are configured via the spice.messageGenerator
// git config value and executed in the repository root directory.
//
// If a script starts with a shebang line (#!),
// it is written to a temporary file and executed directly.
// Otherwise, it is passed to 'sh -c'.
//
// The invoking process's argument vector is forwarded
// to the script as positional parameters.
package msggen

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
)

// Result holds the parsed output of a message generator script.
type Result struct {
	// Title is the first line of output.
	// For commit scripts, this is the commit subject.
	// For branch scripts, this is the PR title.
	Title string

	// Body is the remaining output after the first blank line.
	// For commit scripts, this is the commit body.
	// For branch scripts, this is the PR body.
	Body string
}

// Message returns the full message
// by combining Title and Body in the conventional format:
// title, blank line, body.
//
// If Body is empty, only the Title is returned.
func (r *Result) Message() string {
	if r.Body == "" {
		return r.Title
	}
	return r.Title + "\n\n" + r.Body
}

// ErrNoGenerator is returned when --fill is requested
// but no message generator script is configured.
var ErrNoGenerator = errors.New(
	"--fill requires a message generator script; " +
		"configure one with: " +
		"git config spice.messageGenerator '<script>'",
)

// Runner executes message generator and updater scripts.
type Runner struct {
	// Log is the logger used for debug and warning messages.
	Log *silog.Logger

	// Args is the invoking process's argument vector
	// (typically os.Args).
	// These are forwarded to the script
	// as positional parameters.
	Args []string
}

// Run executes the given script in the specified directory
// with the provided environment variables.
//
// The script is executed with 'sh -c' by default.
// If the script starts with a shebang (#!),
// it is written to a temporary file and executed directly.
//
// The output is parsed into a [Result]:
// the first line becomes the Title,
// and everything after the first blank line becomes the Body.
//
// An error is returned if the script exits with a non-zero status
// or produces no output.
func (r *Runner) Run(
	ctx context.Context,
	script string,
	dir string,
	env []string,
) (*Result, error) {
	cmd, cleanup, err := r.buildCmd(ctx, script)
	if err != nil {
		return nil, fmt.Errorf("build command: %w", err)
	}
	defer cleanup()

	cmd.WithDir(dir).
		AppendEnv("GIT_OPTIONAL_LOCKS=0").
		AppendEnv(env...)

	r.Log.Infof("Generating message...")
	r.Log.Debug("Running message script",
		"script", script,
		"dir", dir,
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run script: %w", err)
	}

	output := strings.TrimSpace(string(out))
	if output == "" {
		return nil, errors.New("script produced no output")
	}

	return parseOutput(output), nil
}

// buildCmd creates an [xec.Cmd] for the given script.
// It returns the command and a cleanup function
// that must be called after the command completes.
func (r *Runner) buildCmd(
	ctx context.Context,
	script string,
) (cmd *xec.Cmd, cleanup func(), _ error) {
	if strings.HasPrefix(script, "#!") {
		return r.buildShebangCmd(ctx, script)
	}
	args := make([]string, 0, 2+len(r.Args))
	args = append(args, "-c", script)
	args = append(args, r.Args...)
	return xec.Command(ctx, r.Log, "sh", args...),
		func() {},
		nil
}

// buildShebangCmd writes the script to a temporary file
// and returns a command that executes it
// using the interpreter from the shebang line.
func (r *Runner) buildShebangCmd(
	ctx context.Context,
	script string,
) (*xec.Cmd, func(), error) {
	f, err := os.CreateTemp("", "gs-msggen-*.sh")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp file: %w", err)
	}

	cleanup := func() {
		_ = os.Remove(f.Name())
	}

	if _, err := f.WriteString(script); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("write script: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("close script: %w", err)
	}
	if err := os.Chmod(f.Name(), 0o700); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("chmod script: %w", err)
	}

	args := make([]string, len(r.Args))
	copy(args, r.Args)
	return xec.Command(ctx, r.Log, f.Name(), args...),
		cleanup, nil
}

// parseOutput splits script output into title and body.
//
// The first line is the title.
// Everything after the first blank line is the body.
// Content between the first line and the first blank line
// is ignored (allows for a separator line).
func parseOutput(output string) *Result {
	// Split on the first blank line.
	title, body, _ := strings.Cut(output, "\n\n")

	// Title is the first line only.
	title, _, _ = strings.Cut(title, "\n")

	return &Result{
		Title: strings.TrimSpace(title),
		Body:  strings.TrimSpace(body),
	}
}

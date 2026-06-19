// Package msggen runs the user-configured message generator script
// and parses its JSON output into a [Result].
//
// Scripts are configured via spice.message.generator. Execution is
// delegated to [scriptrun.Runner] -- the same runner the auto-resolve
// features use -- so the message generator speaks the same JSON
// protocol as the resolvers (see doc/src/guide/scripts.md).
//
// The result fields msggen cares about are Title and Body. The
// shared protocol's Assumptions and Questions are surfaced as logged
// info messages and (when a prompter is wired) an interactive Q&A
// loop driven by the caller.
package msggen

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog"
)

// Result holds the parsed output of a message generator script.
//
// Title and Body are the user-facing message fields. Assumptions are
// short notes the script wants surfaced (logged at info level by the
// caller). Questions, when non-empty, drives a clarification loop the
// caller may run before treating the result as final.
type Result struct {
	// Title is the first-line commit subject or PR title.
	Title string

	// Body is the multi-line message body.
	Body string

	// Assumptions are informational notes the script made; logged at
	// info level by the caller.
	Assumptions []string

	// Questions, when non-empty, asks the user for clarification
	// before the result is treated as final.
	Questions []string
}

// Message returns the full message by combining Title and Body in the
// conventional format: title, blank line, body. If Body is empty,
// only the Title is returned.
func (r *Result) Message() string {
	if r.Body == "" {
		return r.Title
	}
	return r.Title + "\n\n" + r.Body
}

// ErrNoGenerator is returned when --fill is requested but no message
// generator script is configured.
var ErrNoGenerator = errors.New(
	"--fill requires a message generator script; " +
		"configure one with: " +
		"git config spice.message.generator '<script>'",
)

// ScriptRunner is the subset of [scriptrun.Runner] msggen uses. It is
// named so tests can supply a fake.
type ScriptRunner interface {
	Run(ctx context.Context, req *scriptrun.RunRequest) (*scriptrun.RunResult, error)
}

var _ ScriptRunner = (*scriptrun.Runner)(nil)

// Runner executes a message generator script and parses its JSON
// output.
type Runner struct {
	// Log is the logger used for info, warn, and debug messages.
	Log *silog.Logger

	// Args is the invoking process's argument vector (typically
	// os.Args). Forwarded to the script as positional parameters via
	// scriptrun.
	Args []string
}

// scriptOutputPreviewBytes is how much script stdout/stderr is
// included in error diagnostics on parse / non-zero exit.
const scriptOutputPreviewBytes = 2048

// Run executes the given script in dir with the provided extra env
// vars layered on top of the parent environment.
//
// The script speaks the shared scriptrun.ResolveResponse protocol on
// stdout; this method returns the title/body/assumptions/questions
// fields a message-generation caller cares about. Errors are returned
// for: invalid runner state, non-zero script exit, empty stdout,
// invalid JSON, or a JSON document without a Title field.
func (r *Runner) Run(
	ctx context.Context,
	script string,
	dir string,
	env []string,
) (*Result, error) {
	if r.Log == nil {
		r.Log = silog.Nop()
	}
	if script == "" {
		return nil, errors.New("empty script")
	}

	r.Log.Info("Generating message...")
	r.Log.Debug("Running message script",
		"script", script,
		"dir", dir,
	)

	runner := &scriptrun.Runner{Log: r.Log, Args: r.Args}
	res, err := runner.Run(ctx, &scriptrun.RunRequest{
		Script: script,
		Dir:    dir,
		Env:    append([]string{"GIT_OPTIONAL_LOCKS=0"}, env...),
	})
	if err != nil {
		return nil, fmt.Errorf("run script: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf(
			"message script exited with code %d: %s",
			res.ExitCode, previewBytes(res.Stderr),
		)
	}

	resp, err := scriptrun.ParseResponse(res.Stdout)
	if err != nil {
		return nil, fmt.Errorf("parse script output: %w (stdout: %s)",
			err, previewBytes(res.Stdout),
		)
	}
	if resp.Title == "" {
		return nil, errors.New(
			"message script returned no title; the JSON response must include a non-empty 'title' field",
		)
	}

	for _, a := range resp.Assumptions {
		r.Log.Infof("Message: %s", a)
	}

	return &Result{
		Title:       resp.Title,
		Body:        resp.Body,
		Assumptions: resp.Assumptions,
		Questions:   resp.Questions,
	}, nil
}

func previewBytes(b []byte) string {
	if len(b) <= scriptOutputPreviewBytes {
		return string(b)
	}
	return string(b[:scriptOutputPreviewBytes]) + "...[truncated]"
}

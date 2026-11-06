// Package scriptrun executes user-configured shell scripts and
// captures their output.
//
// Scripts are written by the user as small shell programs (typically
// wrapping an external tool like an editor, AI assistant, or test
// runner). git-spice invokes them from a known directory with known
// arguments and environment, then inspects their stdout, stderr, and
// exit code.
//
// # Execution mode
//
// If a script starts with a shebang line (#!), it is written to a
// temporary file (with executable permission) and executed directly,
// letting the kernel use the interpreter from the shebang.
//
// If a script is the path to a regular file, it is executed directly.
//
// Otherwise, the script is passed to 'sh -c'. RunRequest.Args are forwarded
// as positional parameters ($1, $2, ...).
//
// # Output handling
//
// stdout and stderr are captured independently
// unless callers provide explicit output writers.
// Explicit writers receive output during execution
// and opt that stream out of result capture.
// Neither stream is parsed by scriptrun itself:
// the caller decides what to do with the output.
//
// A non-zero exit code is not an error from Run's perspective: it is
// reported in the returned RunResult. Errors are reserved for cases
// where the script could not be started, written, or waited on.
package scriptrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
)

// Runner executes shell scripts.
//
// Runner is safe to reuse across calls but is not safe for concurrent
// use.
type Runner struct {
	// Log receives debug messages and is forwarded to xec.
	// Defaults to a no-op logger if nil.
	Log *silog.Logger
}

// RunRequest is a single script invocation.
type RunRequest struct {
	// Script is the script body to execute.
	// If it starts with "#!", it is executed directly via the
	// interpreter named in the shebang. If it names a regular file,
	// that file is executed directly. Otherwise, it is passed to
	// "sh -c".
	Script string // required

	// Args is forwarded to the script as positional parameters.
	Args []string

	// Dir is the working directory for the script.
	// Empty means the current working directory.
	Dir string

	// Env lists additional environment variables to set, in the
	// "KEY=value" form. These are appended to the parent process's
	// environment.
	Env []string

	// Stdin, if non-nil, is supplied to the script as its stdin.
	Stdin io.Reader

	// Stdout, if non-nil, receives stdout while the script is running.
	// RunResult.Stdout is not populated in that case.
	Stdout io.Writer

	// Stderr, if non-nil, receives stderr while the script is running.
	// RunResult.Stderr is not populated in that case.
	Stderr io.Writer
}

// RunResult is the outcome of a single script invocation.
type RunResult struct {
	// ExitCode is the script's exit code. Zero indicates success.
	ExitCode int

	// Stdout is the captured standard output of the script.
	// If RunRequest.Stdout was set, Stdout is not populated.
	Stdout []byte

	// Stderr is the captured standard error of the script.
	// If RunRequest.Stderr was set, Stderr is not populated.
	Stderr []byte
}

// Run executes the script described by req and returns its outcome.
//
// A non-zero exit code is reported in RunResult.ExitCode, not as an
// error. Run returns an error only if the script could not be started
// or waited on (e.g., temp-file creation failed, or the executable
// could not be found).
func (r *Runner) Run(ctx context.Context, req *RunRequest) (*RunResult, error) {
	must.NotBeNilf(req, "scriptrun: nil request")
	if req.Script == "" {
		return nil, errors.New("scriptrun: empty script")
	}

	log := r.Log
	if log == nil {
		log = silog.Nop()
	}

	cmd, cleanup, err := r.buildCmd(ctx, log, req.Script, req.Args)
	if err != nil {
		return nil, fmt.Errorf("build command: %w", err)
	}
	defer cleanup()

	if req.Dir != "" {
		cmd.WithDir(req.Dir)
	}
	if len(req.Env) > 0 {
		cmd.AppendEnv(req.Env...)
	}
	if req.Stdin != nil {
		cmd.WithStdin(req.Stdin)
	}
	cmd.WithWaitDelay(100 * time.Millisecond)

	var stdout, stderr bytes.Buffer
	stdoutWriter := io.Writer(&stdout)
	if req.Stdout != nil {
		stdoutWriter = req.Stdout
	}
	stderrWriter := io.Writer(&stderr)
	if req.Stderr != nil {
		stderrWriter = req.Stderr
	}
	cmd.WithStdout(stdoutWriter).WithStderr(stderrWriter)

	runErr := cmd.Run()
	if runErr != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	exitErr := new(xec.ExitError)
	switch {
	case runErr == nil:
		return &RunResult{
			ExitCode: 0,
			Stdout:   stdout.Bytes(),
			Stderr:   stderr.Bytes(),
		}, nil
	case errors.As(runErr, &exitErr):
		return &RunResult{
			ExitCode: exitErr.ExitCode(),
			Stdout:   stdout.Bytes(),
			Stderr:   stderr.Bytes(),
		}, nil
	default:
		return nil, fmt.Errorf("run script: %w", runErr)
	}
}

// buildCmd creates an [xec.Cmd] for the given script. The returned
// cleanup function must be invoked once the command completes.
func (r *Runner) buildCmd(
	ctx context.Context,
	log *silog.Logger,
	script string,
	scriptArgs []string,
) (*xec.Cmd, func(), error) {
	if strings.HasPrefix(script, "#!") {
		return r.buildShebangCmd(ctx, log, script, scriptArgs)
	}
	if info, err := os.Stat(script); err == nil && info.Mode().IsRegular() {
		return xec.Command(ctx, log, script, scriptArgs...), func() {}, nil
	}
	args := make([]string, 0, 3+len(scriptArgs))
	args = append(args, "-c", script, "gs-scriptrun")
	args = append(args, scriptArgs...)
	return xec.Command(ctx, log, "sh", args...), func() {}, nil
}

// buildShebangCmd writes the script to a temporary file and returns a
// command that executes it directly.
//
// Positional parameters are aligned with the "sh -c" form: $1 is the
// first Runner argument.
func (r *Runner) buildShebangCmd(
	ctx context.Context,
	log *silog.Logger,
	script string,
	scriptArgs []string,
) (cmd *xec.Cmd, cleanup func(), err error) {
	f, err := os.CreateTemp("", "gs-scriptrun-*.sh")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp file: %w", err)
	}
	cleanup = func() {
		_ = os.Remove(f.Name())
	}
	defer func() {
		if err != nil {
			cleanup()
			cleanup = nil
		}
	}()

	if _, err := f.WriteString(script); err != nil {
		return nil, nil, fmt.Errorf("write script: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, nil, fmt.Errorf("close script: %w", err)
	}
	if err := os.Chmod(f.Name(), 0o700); err != nil {
		return nil, nil, fmt.Errorf("chmod script: %w", err)
	}

	return xec.Command(ctx, log, f.Name(), scriptArgs...), cleanup, nil
}

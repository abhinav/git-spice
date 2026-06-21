package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/spicedir"
)

// Resolver attempts to resolve the in-progress merge conflict.
//
// Successful return indicates the resolver ran to completion with a
// well-formed response. The shape of the response indicates whether
// the conflict was actually resolved.
type Resolver interface {
	Resolve(ctx context.Context, req *ResolveRequest) (*scriptrun.ResolveResponse, error)
}

// ResolveRequest describes the conflict that the resolver should
// attempt to resolve.
type ResolveRequest struct {
	// IntegrationName is the local branch name of the integration
	// branch (also known as "ours" during the in-progress merge).
	IntegrationName string

	// TipName is the branch name being merged into the integration
	// branch (also known as "theirs").
	TipName string
}

// ScriptRunner is the subset of [scriptrun.Runner] used by the
// resolver. It is named so tests can supply a fake.
type ScriptRunner interface {
	Run(ctx context.Context, req *scriptrun.RunRequest) (*scriptrun.RunResult, error)
}

var _ ScriptRunner = (*scriptrun.Runner)(nil)

// ScriptResolver invokes a user-configured shell script and parses
// the JSON document it produces on stdout.
type ScriptResolver struct {
	// Log receives diagnostic messages.
	Log *silog.Logger

	// Script is the resolver script body.
	Script string

	// Runner is the [ScriptRunner] used to execute Script.
	Runner ScriptRunner

	// RepoRoot is the directory containing the resolution file.
	RepoRoot string
}

// ScriptResolveError indicates that the script ran but its output did
// not conform to the expected JSON protocol. The captured stdout and
// stderr are preserved so callers can surface them to the user.
type ScriptResolveError struct {
	// Stage describes what went wrong (e.g., "exit", "parse").
	Stage string

	// ExitCode is the script's exit code.
	ExitCode int

	// Stdout is the captured standard output of the script.
	Stdout []byte

	// Stderr is the captured standard error of the script.
	Stderr []byte

	// Err is the underlying error, if any.
	Err error
}

func (e *ScriptResolveError) Error() string {
	var msg string
	switch e.Stage {
	case "exit":
		msg = fmt.Sprintf("resolver exited with code %d", e.ExitCode)
	case "parse":
		msg = fmt.Sprintf("resolver output is not valid JSON: %v", e.Err)
	default:
		msg = fmt.Sprintf("resolver failed: %v", e.Err)
	}
	return msg + e.diagnosticPreview()
}

// diagnosticPreview returns a truncated preview of the resolver's
// stdout and stderr for inclusion in the error message. Each stream
// is capped at scriptOutputPreviewBytes to keep the log readable;
// truncated streams are marked with a leading note. Empty streams
// are explicitly labeled so the user knows whether a stream was
// missing or unbounded.
func (e *ScriptResolveError) diagnosticPreview() string {
	if len(e.Stdout) == 0 && len(e.Stderr) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n--- resolver stdout ")
	writeOutputPreview(&b, e.Stdout)
	b.WriteString("\n--- resolver stderr ")
	writeOutputPreview(&b, e.Stderr)
	return b.String()
}

const scriptOutputPreviewBytes = 2048

func writeOutputPreview(b *strings.Builder, out []byte) {
	if len(out) == 0 {
		b.WriteString("(empty) ---")
		return
	}
	if len(out) <= scriptOutputPreviewBytes {
		b.WriteString("---\n")
		b.Write(bytes.TrimRight(out, "\n"))
		return
	}
	fmt.Fprintf(b, "(last %d of %d bytes) ---\n",
		scriptOutputPreviewBytes, len(out))
	b.Write(bytes.TrimRight(out[len(out)-scriptOutputPreviewBytes:], "\n"))
}

func (e *ScriptResolveError) Unwrap() error { return e.Err }

// Resolve writes the current_merge pointer to the resolution file,
// invokes the script, and parses its stdout as JSON.
func (r *ScriptResolver) Resolve(
	ctx context.Context, req *ResolveRequest,
) (*scriptrun.ResolveResponse, error) {
	if req == nil {
		return nil, errors.New("nil resolve request")
	}
	if r.Script == "" {
		return nil, errors.New("no resolver script configured")
	}

	pair := MergePair{Ours: req.IntegrationName, Theirs: req.TipName}
	if err := r.writeCurrentMerge(pair); err != nil {
		return nil, fmt.Errorf("update resolution file: %w", err)
	}

	res, err := r.Runner.Run(ctx, &scriptrun.RunRequest{
		Script: r.Script,
		Dir:    r.RepoRoot,
		Env: scriptrun.EnvFor(
			scriptrun.OpIntegrationRebuild,
			req.TipName,         // GS_BRANCH = the tip being merged in
			req.IntegrationName, // GS_BASE   = the integration branch
		),
	})
	if err != nil {
		return nil, fmt.Errorf("run resolver: %w", err)
	}

	if res.ExitCode != 0 {
		return nil, &ScriptResolveError{
			Stage:    "exit",
			ExitCode: res.ExitCode,
			Stdout:   res.Stdout,
			Stderr:   res.Stderr,
		}
	}

	resp, err := scriptrun.ParseResponse(res.Stdout)
	if err != nil {
		return nil, &ScriptResolveError{
			Stage:    "parse",
			ExitCode: res.ExitCode,
			Stdout:   res.Stdout,
			Stderr:   res.Stderr,
			Err:      err,
		}
	}
	return resp, nil
}

// writeCurrentMerge updates the resolution file's current_merge
// pointer to pair, preserving existing resolutions. Creates the file
// (and the parent .spice/resolutions directory) if absent.
func (r *ScriptResolver) writeCurrentMerge(pair MergePair) error {
	if err := spicedir.EnsureResolutionsDir(r.RepoRoot); err != nil {
		return err
	}
	path := spicedir.ResolutionPath(r.RepoRoot, ResolutionFeatureName)
	file, err := LoadResolutionFile(path)
	if err != nil {
		return err
	}
	file.CurrentMerge = &pair
	file.EnsureEntry(pair)
	return file.Save(path)
}

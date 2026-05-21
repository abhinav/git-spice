package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog"
)

// Resolver attempts to resolve the in-progress merge conflict.
//
// Successful return indicates the resolver ran to completion with a
// well-formed response. The shape of the response indicates whether
// the conflict was actually resolved.
type Resolver interface {
	Resolve(ctx context.Context, req *ResolveRequest) (*ResolveResponse, error)
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

// ResolveResponse is the parsed JSON output of the resolver script.
//
// All fields are optional; an empty response means "everything
// resolved cleanly, no remarks."
type ResolveResponse struct {
	// Assumptions are informational notes the resolver made during
	// its work. Logged at info level by the caller.
	Assumptions []string `json:"assumptions,omitempty"`

	// Questions are unresolved decisions the resolver wants the user
	// to answer. The caller prompts the user, appends Q&A to the
	// resolution file, and re-invokes the resolver.
	Questions []string `json:"questions,omitempty"`

	// UnresolvedFiles lists paths the resolver could not resolve.
	// Combined with Questions, the caller may iterate. With no
	// Questions, the caller surfaces an error and asks the user to
	// investigate manually.
	UnresolvedFiles []string `json:"unresolved_files,omitempty"`
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
	switch e.Stage {
	case "exit":
		return fmt.Sprintf(
			"resolver exited with code %d", e.ExitCode)
	case "parse":
		return fmt.Sprintf(
			"resolver output is not valid JSON: %v", e.Err)
	default:
		return fmt.Sprintf("resolver failed: %v", e.Err)
	}
}

func (e *ScriptResolveError) Unwrap() error { return e.Err }

// Resolve writes the current_merge pointer to the resolution file,
// invokes the script, and parses its stdout as JSON.
func (r *ScriptResolver) Resolve(
	ctx context.Context, req *ResolveRequest,
) (*ResolveResponse, error) {
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

	stdout := bytes.TrimSpace(res.Stdout)
	if len(stdout) == 0 {
		return nil, &ScriptResolveError{
			Stage:    "parse",
			ExitCode: res.ExitCode,
			Stdout:   res.Stdout,
			Stderr:   res.Stderr,
			Err:      errors.New("empty output"),
		}
	}

	var resp ResolveResponse
	dec := json.NewDecoder(bytes.NewReader(stdout))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&resp); err != nil {
		return nil, &ScriptResolveError{
			Stage:    "parse",
			ExitCode: res.ExitCode,
			Stdout:   res.Stdout,
			Stderr:   res.Stderr,
			Err:      err,
		}
	}

	return &resp, nil
}

// writeCurrentMerge updates the resolution file's current_merge
// pointer to pair, preserving existing resolutions. Creates the file
// if it does not exist.
func (r *ScriptResolver) writeCurrentMerge(pair MergePair) error {
	path := filepath.Join(r.RepoRoot, ResolutionFileName)
	file, err := LoadResolutionFile(path)
	if err != nil {
		return err
	}
	file.CurrentMerge = &pair
	file.EnsureEntry(pair) // make sure an entry exists for the script.
	return file.Save(path)
}

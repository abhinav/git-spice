package restack

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/spicedir"
)

// Resolver attempts to resolve the in-progress rebase conflict.
//
// Successful return indicates the resolver ran to completion with a
// well-formed response. The shape of the response indicates whether
// the conflict was actually resolved.
type Resolver interface {
	Resolve(ctx context.Context, req *ResolveRequest) (*scriptrun.ResolveResponse, error)
}

// ResolveRequest describes the conflict that the resolver should
// attempt to resolve.
//
// During a rebase, "ours" is the base (where we are replaying onto)
// and "theirs" is the branch whose commits are being replayed. The
// resolution-file entry keyed by (Base, Branch) accumulates Q&A
// across rebases, so once the user has answered a question for a
// given pair, the answer is reused on the next restack.
type ResolveRequest struct {
	// Operation identifies which gs subcommand drove this resolve
	// (e.g. branch-restack, stack-restack). Forwarded to the script
	// as GS_OPERATION via [scriptrun.EnvFor].
	Operation scriptrun.Operation

	// Base is the branch the rebase is replaying onto (also known
	// as "ours" during the in-progress rebase).
	Base string

	// Branch is the branch whose commits are being replayed (also
	// known as "theirs").
	Branch string
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
) (*scriptrun.ResolveResponse, error) {
	if req == nil {
		return nil, errors.New("nil resolve request")
	}
	if r.Script == "" {
		return nil, errors.New("no resolver script configured")
	}

	pair := MergePair{Ours: req.Base, Theirs: req.Branch}
	if err := r.writeCurrentMerge(pair); err != nil {
		return nil, fmt.Errorf("update resolution file: %w", err)
	}

	res, err := r.Runner.Run(ctx, &scriptrun.RunRequest{
		Script: r.Script,
		Dir:    r.RepoRoot,
		Env:    scriptrun.EnvFor(req.Operation, req.Branch, req.Base),
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

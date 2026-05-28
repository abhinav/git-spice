package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog"
)

// RegenerateFileName is the well-known path (relative to the repo
// root) of the post-merge regenerator script invoked by gs after a
// successful integration rebuild. If absent or non-executable, gs
// silently skips the regeneration step.
const RegenerateFileName = ".gs/integration-regenerate"

// Regenerator runs a project-level script with a list of paths whose
// conflicts the regenerate merge driver resolved by taking incoming.
//
// The script is expected to re-derive any of those paths that need it
// (for example, by re-running mockgen if a mock file was in the list).
// gs hands the script the list on stdin, in repo-root cwd; the
// script's output (worktree modifications) is folded into the last
// merge commit by the caller via Worktree.AmendCommitAll.
type Regenerator interface {
	Regenerate(ctx context.Context, paths []string) error
}

// ScriptRunner is the subset of [scriptrun.Runner] used by
// [FileRegenerator]. It is named here so tests can supply a fake
// without taking a dependency on scriptrun's concrete type.
//
// [ScriptResolver] (in resolver.go) declares its own ScriptRunner
// alias; both refer to the same shape. The interface lives in this
// file too to avoid coupling regenerator.go to resolver.go.
type regeneratorScriptRunner interface {
	Run(ctx context.Context, req *scriptrun.RunRequest) (*scriptrun.RunResult, error)
}

// FileRegenerator looks for [RegenerateFileName] in the repo root.
// If it exists and is executable, FileRegenerator reads its contents
// and invokes the script via [scriptrun.Runner] with the list of
// paths on stdin.
//
// If the file does not exist, [FileRegenerator.Regenerate] returns
// nil (no-op). This is intentional: projects without a regenerator
// script get the take-incoming merge driver behavior with no extra
// overhead.
type FileRegenerator struct {
	Log      *silog.Logger
	Runner   regeneratorScriptRunner
	RepoRoot string
}

// Regenerate reads the project regenerator script and invokes it with
// the deduped path list on stdin.
//
// Returns nil if the script does not exist. Returns an error if the
// script exists but is not executable, if it cannot be read, or if it
// exits with a non-zero code. The caller decides whether to treat
// the error as fatal or a warning.
func (r *FileRegenerator) Regenerate(
	ctx context.Context, paths []string,
) error {
	scriptPath := filepath.Join(r.RepoRoot, RegenerateFileName)
	info, err := os.Stat(scriptPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("stat %s: %w", RegenerateFileName, err)
	}

	if info.Mode()&0o111 == 0 {
		return fmt.Errorf(
			"%s exists but is not executable; chmod +x to enable",
			RegenerateFileName)
	}

	body, err := os.ReadFile(scriptPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", RegenerateFileName, err)
	}

	var stdin bytes.Buffer
	for _, p := range paths {
		stdin.WriteString(p)
		stdin.WriteByte('\n')
	}

	res, err := r.Runner.Run(ctx, &scriptrun.RunRequest{
		Script: string(body),
		Dir:    r.RepoRoot,
		Stdin:  &stdin,
	})
	if err != nil {
		return fmt.Errorf("run %s: %w", RegenerateFileName, err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%s exited with code %d: %s",
			RegenerateFileName, res.ExitCode,
			bytes.TrimSpace(res.Stderr))
	}
	return nil
}

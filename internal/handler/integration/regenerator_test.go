package integration_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/handler/integration"
	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog"
)

// fakeRegenRunner records the most recent script invocation and
// returns a canned result. Tests use it to inspect what the
// regenerator sent to the script.
type fakeRegenRunner struct {
	lastScript string
	lastDir    string
	lastStdin  string
	result     *scriptrun.RunResult
	err        error
}

func (f *fakeRegenRunner) Run(
	_ context.Context, req *scriptrun.RunRequest,
) (*scriptrun.RunResult, error) {
	f.lastScript = req.Script
	f.lastDir = req.Dir
	if req.Stdin != nil {
		data, _ := io.ReadAll(req.Stdin)
		f.lastStdin = string(data)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o700))
}

func TestFileRegenerator_absent(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRegenRunner{}
	r := &integration.FileRegenerator{
		Log:      silog.Nop(),
		Runner:   runner,
		RepoRoot: dir,
	}

	require.NoError(t, r.Regenerate(t.Context(), []string{"a", "b"}))
	assert.Empty(t, runner.lastScript,
		"runner must not be invoked when script is absent")
}

func TestFileRegenerator_notExecutable(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, integration.RegenerateFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(scriptPath), 0o755))
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\n"), 0o600))

	r := &integration.FileRegenerator{
		Log:      silog.Nop(),
		Runner:   &fakeRegenRunner{},
		RepoRoot: dir,
	}

	err := r.Regenerate(t.Context(), []string{"a"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not executable")
}

func TestFileRegenerator_invokesScript(t *testing.T) {
	dir := t.TempDir()
	scriptBody := "#!/bin/sh\necho stub\n"
	writeExecutable(t,
		filepath.Join(dir, integration.RegenerateFileName), scriptBody)

	runner := &fakeRegenRunner{
		result: &scriptrun.RunResult{ExitCode: 0},
	}
	r := &integration.FileRegenerator{
		Log:      silog.Nop(),
		Runner:   runner,
		RepoRoot: dir,
	}

	require.NoError(t,
		r.Regenerate(t.Context(), []string{"foo.go", "bar.go"}))

	assert.Equal(t, scriptBody, runner.lastScript)
	assert.Equal(t, dir, runner.lastDir)
	assert.Equal(t, "foo.go\nbar.go\n", runner.lastStdin)
}

func TestFileRegenerator_emptyPaths(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t,
		filepath.Join(dir, integration.RegenerateFileName),
		"#!/bin/sh\n")

	runner := &fakeRegenRunner{
		result: &scriptrun.RunResult{ExitCode: 0},
	}
	r := &integration.FileRegenerator{
		Log:      silog.Nop(),
		Runner:   runner,
		RepoRoot: dir,
	}

	// Empty path list: the script is still invoked (the handler
	// decides whether to call us; we don't second-guess) but stdin
	// is empty.
	require.NoError(t, r.Regenerate(t.Context(), nil))
	assert.Empty(t, runner.lastStdin)
}

func TestFileRegenerator_nonZeroExit(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t,
		filepath.Join(dir, integration.RegenerateFileName),
		"#!/bin/sh\n")

	runner := &fakeRegenRunner{
		result: &scriptrun.RunResult{
			ExitCode: 3,
			Stderr:   []byte("regen blew up"),
		},
	}
	r := &integration.FileRegenerator{
		Log:      silog.Nop(),
		Runner:   runner,
		RepoRoot: dir,
	}

	err := r.Regenerate(t.Context(), []string{"foo"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exited with code 3")
	assert.Contains(t, err.Error(), "regen blew up")
}

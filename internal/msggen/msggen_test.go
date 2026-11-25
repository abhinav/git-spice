package msggen

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestHandler_Generate(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	t.Setenv("MSGGEN_TEST_HELPER", "range")
	handler := &Handler{
		Fill:    true,
		Command: exe,
		Runner:  &scriptrun.Runner{Log: silogtest.New(t)},
	}
	message, err := handler.Generate(t.Context(), &RangeRequest{
		Operation: BranchSquash,
		Base:      git.Hash("base"),
		Head:      git.Hash("head"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Subject\n\nBody\n", message)
}

func TestHandler_Generate_missingCommand(t *testing.T) {
	handler := &Handler{Fill: true, Runner: &scriptrun.Runner{Log: silogtest.New(t)}}
	_, err := handler.Generate(t.Context(), &RangeRequest{
		Operation: BranchSubmit,
		Base:      "base",
		Head:      "head",
	})
	require.ErrorIs(t, err, ErrNoCommand)
}

func TestHandler_Generate_failures(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	for _, tt := range []struct{ name, mode, message string }{
		{name: "NonzeroExit", mode: "exit", message: "status 7"},
		{name: "StdoutOnly", mode: "stdout-only", message: "non-empty subject"},
		{name: "BlankSubject", mode: "blank-subject", message: "non-empty subject"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MSGGEN_TEST_HELPER", tt.mode)
			handler := &Handler{Fill: true, Command: exe, Runner: &scriptrun.Runner{Log: silogtest.New(t)}}
			_, err := handler.Generate(t.Context(), &RangeRequest{
				Operation: BranchSubmit, Base: "base", Head: "head",
			})
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestHandler_Generate_executable(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	t.Setenv("MSGGEN_TEST_HELPER", "executable")
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	dir := filepath.Join(root, "worktree")
	require.NoError(t, os.Mkdir(bin, 0o700))
	require.NoError(t, os.Mkdir(dir, 0o700))
	installMessageTestExecutable(t, exe, bin)
	command := installMessageTestExecutable(t, exe, dir)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, tt := range []struct{ name, command, message string }{
		{name: "OnPath", command: "fill-message", message: "bin\n"},
		{name: "AbsolutePath", command: command, message: "worktree\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler := &Handler{Fill: true, Command: tt.command, Dir: dir, Runner: &scriptrun.Runner{Log: silogtest.New(t)}}
			message, err := handler.Generate(t.Context(), &RangeRequest{
				Operation: BranchSubmit, Base: "base", Head: "head",
			})
			require.NoError(t, err)
			assert.Equal(t, tt.message, message)
		})
	}
}

func TestHandler_Generate_cancelled(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	t.Setenv("MSGGEN_TEST_HELPER", "range")
	handler := &Handler{Fill: true, Command: exe, Runner: &scriptrun.Runner{Log: silogtest.New(t)}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = handler.Generate(ctx, &RangeRequest{
		Operation: BranchSquash, Base: "base", Head: "head",
	})
	require.ErrorIs(t, err, context.Canceled)
}

func installMessageTestExecutable(t *testing.T, source, dir string) string {
	t.Helper()
	name := "fill-message"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.Symlink(source, path); err != nil {
		data, err := os.ReadFile(source)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0o700))
	}
	return path
}

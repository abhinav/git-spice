package msggen

import (
	"context"
	"os"
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

func TestHandler_Generate_unchangedDraft(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	t.Setenv("MSGGEN_TEST_HELPER", "unchanged")
	handler := &Handler{
		Fill:    true,
		Command: exe,
		Runner:  &scriptrun.Runner{Log: silogtest.New(t)},
	}
	draft := "Subject\n\nBody\n"
	message, err := handler.Generate(t.Context(), &RangeRequest{
		Operation: BranchSubmit,
		Base:      "base",
		Head:      "head",
		Draft:     draft,
	})
	require.NoError(t, err)
	assert.Equal(t, draft, message)
}

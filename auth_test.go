package main

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/ui"
	"go.uber.org/mock/gomock"
)

func TestResolveForge_explicitForgeWins(t *testing.T) {
	ctrl := gomock.NewController(t)

	github := forgetest.NewMockForge(ctrl)
	github.EXPECT().ID().Return("github").AnyTimes()
	gitlab := forgetest.NewMockForge(ctrl)
	gitlab.EXPECT().ID().Return("gitlab").AnyTimes()

	var forges forge.Registry
	forges.Register(github)
	forges.Register(gitlab)

	got, err := resolveForge(
		t.Context(),
		&forges,
		silogtest.New(t),
		ui.NewFileView(io.Discard),
		"gitlab",
		"github",
	)
	require.NoError(t, err)

	assert.Same(t, gitlab, got)
}

func TestResolveForge_configuredKind(t *testing.T) {
	ctrl := gomock.NewController(t)

	github := forgetest.NewMockForge(ctrl)
	github.EXPECT().ID().Return("github").AnyTimes()
	gitlab := forgetest.NewMockForge(ctrl)
	gitlab.EXPECT().ID().Return("gitlab").AnyTimes()

	var forges forge.Registry
	forges.Register(github)
	forges.Register(gitlab)

	got, err := resolveForge(
		t.Context(),
		&forges,
		silogtest.New(t),
		ui.NewFileView(io.Discard),
		"",
		"github",
	)
	require.NoError(t, err)

	assert.Same(t, github, got)
}

func TestResolveForge_noForgeSignalPreservesNoninteractiveError(t *testing.T) {
	t.Chdir(t.TempDir())

	ctrl := gomock.NewController(t)

	github := forgetest.NewMockForge(ctrl)
	github.EXPECT().ID().Return("github").AnyTimes()
	gitlab := forgetest.NewMockForge(ctrl)
	gitlab.EXPECT().ID().Return("gitlab").AnyTimes()

	var forges forge.Registry
	forges.Register(github)
	forges.Register(gitlab)

	_, err := resolveForge(
		t.Context(),
		&forges,
		silogtest.New(t),
		ui.NewFileView(io.Discard),
		"",
		"",
	)
	require.Error(t, err)

	assert.ErrorIs(t, err, errNoPrompt)
}

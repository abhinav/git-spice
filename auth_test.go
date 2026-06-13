package main

import (
	"io"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/bitbucket"
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

// TestResolveForge_configuredKindOutsideRepository verifies that
// forge self-configuration from the remote URL is best-effort:
// outside a Git repository there is no remote to derive from,
// so the forge resolves with its default configuration intact.
func TestResolveForge_configuredKindOutsideRepository(t *testing.T) {
	t.Chdir(t.TempDir())

	bb := &bitbucket.Forge{}
	var forges forge.Registry
	forges.Register(bb)

	got, err := resolveForge(
		t.Context(),
		&forges,
		silogtest.New(t),
		ui.NewFileView(io.Discard),
		"",
		"bitbucket",
	)
	require.NoError(t, err)

	assert.Same(t, bb, got)
	assert.Empty(t, bb.Options.URL)
}

func TestResolveForge_configuredKindUsesRemoteConfigURL(t *testing.T) {
	t.Chdir(t.TempDir())
	runGitAuthTest(t, "init")
	runGitAuthTest(t, "remote", "add", "origin",
		"ssh://bitbucket-alias/scm/PROJ/repo.git")
	runGitAuthTest(t, "config", "url.https://git.corp.com/.insteadOf",
		"ssh://bitbucket-alias/")

	bb := &bitbucket.Forge{}
	var forges forge.Registry
	forges.Register(bb)

	got, err := resolveForge(
		t.Context(),
		&forges,
		silogtest.New(t),
		ui.NewFileView(io.Discard),
		"",
		"bitbucket",
	)
	require.NoError(t, err)

	assert.Same(t, bb, got)
	assert.Equal(t, "https://bitbucket-alias", bb.Options.URL)
}

func runGitAuthTest(t *testing.T, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
}

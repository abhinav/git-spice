package main

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/git/giturl"
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
	registerTestForge(&forges, "github", github)
	registerTestForge(&forges, "gitlab", gitlab)

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

func TestResolveForge_explicitForgeWithoutRepositoryRemote(t *testing.T) {
	t.Chdir(t.TempDir())

	ctrl := gomock.NewController(t)

	github := forgetest.NewMockForge(ctrl)
	github.EXPECT().ID().Return("github").AnyTimes()

	var gotRemoteURL *giturl.URL
	var forges forge.Registry
	forges.Register(testDefinition{
		id: "github",
		new: func(remoteURL *giturl.URL) (forge.Forge, error) {
			gotRemoteURL = remoteURL
			return github, nil
		},
	})

	got, err := resolveForge(
		t.Context(),
		&forges,
		silogtest.New(t),
		ui.NewFileView(io.Discard),
		"github",
		"",
	)
	require.NoError(t, err)

	assert.Same(t, github, got)
	require.NotNil(t, gotRemoteURL)
	assert.Equal(t, "https://example.com", gotRemoteURL.Raw)
}

func TestResolveForge_configuredKind(t *testing.T) {
	ctrl := gomock.NewController(t)

	github := forgetest.NewMockForge(ctrl)
	github.EXPECT().ID().Return("github").AnyTimes()
	gitlab := forgetest.NewMockForge(ctrl)
	gitlab.EXPECT().ID().Return("gitlab").AnyTimes()

	var forges forge.Registry
	registerTestForge(&forges, "github", github)
	registerTestForge(&forges, "gitlab", gitlab)

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

func TestResolveForge_requiresGitRemote(t *testing.T) {
	t.Chdir(t.TempDir())

	var forges forge.Registry
	_, err := resolveForge(
		t.Context(),
		&forges,
		silogtest.New(t),
		ui.NewFileView(io.Discard),
		"",
		"",
	)
	require.Error(t, err)

	assert.ErrorContains(t, err, "not in a Git repository")
}

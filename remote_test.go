package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/bitbucket"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/silog"
	"go.uber.org/mock/gomock"
)

func TestRemoteResolver_Resolve(t *testing.T) {
	ctrl := gomock.NewController(t)

	repoID := forgetest.NewMockRepositoryID(ctrl)
	testForge := forgetest.NewMockForge(ctrl)
	testForge.EXPECT().ID().Return("test").AnyTimes()
	testForge.EXPECT().BaseURL().Return("https://example.com")
	testForge.EXPECT().
		ParseRepositoryPath("/owner/repo").
		Return(repoID, nil)

	var forges forge.Registry
	forges.Register(testForge)

	gotForge, gotRepoID, err := (&remoteResolver{
		Forges:     &forges,
		Repository: remoteURLMap{"origin": "https://example.com/owner/repo"},
		ForgeKind:  "",
	}).Resolve(t.Context(), "origin")
	require.NoError(t, err)

	assert.Same(t, testForge, gotForge)
	assert.Equal(t, repoID, gotRepoID)
}

func TestRemoteResolver_ResolveID(t *testing.T) {
	ctrl := gomock.NewController(t)

	repoID := forgetest.NewMockRepositoryID(ctrl)
	testForge := forgetest.NewMockForge(ctrl)
	testForge.EXPECT().ID().Return("test").AnyTimes()
	testForge.EXPECT().BaseURL().Return("https://example.com")
	testForge.EXPECT().
		ParseRepositoryPath("/owner/repo").
		Return(repoID, nil)

	var forges forge.Registry
	forges.Register(testForge)

	gotRepoID, err := (&remoteResolver{
		Forges:     &forges,
		Repository: remoteURLMap{"origin": "https://example.com/owner/repo"},
		ForgeKind:  "",
	}).ResolveID(t.Context(), "origin")
	require.NoError(t, err)

	assert.Equal(t, repoID, gotRepoID)
}

func TestRemoteResolver_Resolve_configuredKind(t *testing.T) {
	ctrl := gomock.NewController(t)

	repoID := forgetest.NewMockRepositoryID(ctrl)
	testForge := forgetest.NewMockForge(ctrl)
	testForge.EXPECT().ID().Return("github").AnyTimes()
	testForge.EXPECT().
		ParseRepositoryPath("/owner/repo.git").
		Return(repoID, nil)

	var forges forge.Registry
	forges.Register(testForge)

	gotForge, gotRepoID, err := (&remoteResolver{
		Forges:     &forges,
		Repository: remoteURLMap{"origin": "ssh://githubaccount1/owner/repo.git"},
		ForgeKind:  "github",
	}).Resolve(t.Context(), "origin")
	require.NoError(t, err)

	assert.Same(t, testForge, gotForge)
	assert.Equal(t, repoID, gotRepoID)
}

func TestRemoteResolver_Resolve_unknownConfiguredKind(t *testing.T) {
	var forges forge.Registry
	_, _, err := (&remoteResolver{
		Forges:     &forges,
		Repository: remoteURLMap{"origin": "ssh://githubaccount1/owner/repo.git"},
		ForgeKind:  "github",
	}).Resolve(t.Context(), "origin")
	require.Error(t, err)

	assert.ErrorContains(t, err, `unknown forge kind "github"`)
}

func TestRemoteResolver_Resolve_unsupported(t *testing.T) {
	var forges forge.Registry
	_, _, err := (&remoteResolver{
		Forges:     &forges,
		Repository: remoteURLMap{"origin": "https://example.com/owner/repo"},
		ForgeKind:  "",
	}).Resolve(t.Context(), "origin")
	require.Error(t, err)

	var unsupported *unsupportedForgeError
	require.ErrorAs(t, err, &unsupported)
	assert.Equal(t, "origin", unsupported.Remote)
	assert.Equal(t, "https://example.com/owner/repo", unsupported.RemoteURL)
}

func TestResolveRemoteRepository_unsupportedRecommendsForgeKind(t *testing.T) {
	var logBuffer bytes.Buffer
	var forges forge.Registry
	_, _, err := resolveRemoteRepository(
		t.Context(),
		silog.New(&logBuffer, nil),
		&remoteResolver{
			Forges:     &forges,
			Repository: remoteURLMap{"origin": "ssh://githubalias/owner/repo.git"},
			ForgeKind:  "",
		},
		"origin",
	)
	require.Error(t, err)

	assert.Contains(t, logBuffer.String(), "git config spice.forge.kind <forge>")
}

// TestRemoteResolver_Resolve_bitbucketKind exercises the forced-kind path
// with the real bitbucket forge,
// which configures its instance URL from the remote
// via [forge.RemoteURLConfigurer].
// Each subtest builds its own Forge and Registry
// because resolution may mutate the forge's options.
func TestRemoteResolver_Resolve_bitbucketKind(t *testing.T) {
	t.Run("DataCenterRemoteDerivesURL", func(t *testing.T) {
		bb := &bitbucket.Forge{}
		var forges forge.Registry
		forges.Register(bb)

		gotForge, gotRepoID, err := (&remoteResolver{
			Forges:     &forges,
			Repository: remoteURLMap{"origin": "https://git.corp.com/scm/PROJ/repo.git"},
			ForgeKind:  "bitbucket",
		}).Resolve(t.Context(), "origin")
		require.NoError(t, err)

		assert.Same(t, bb, gotForge)
		assert.Equal(t, "https://git.corp.com", bb.Options.URL)
		assert.Equal(t, "PROJ/repo", gotRepoID.String())

		// Data Center-shaped change URLs prove that the derived
		// instance URL reached the repository ID.
		assert.Equal(t,
			"https://git.corp.com/projects/PROJ/repos/repo/pull-requests/1/overview",
			gotRepoID.ChangeURL(&bitbucket.PR{Number: 1}))
	})

	t.Run("CloudRemoteKeepsDefault", func(t *testing.T) {
		bb := &bitbucket.Forge{}
		var forges forge.Registry
		forges.Register(bb)

		gotForge, gotRepoID, err := (&remoteResolver{
			Forges:     &forges,
			Repository: remoteURLMap{"origin": "git@bitbucket.org:ws/repo.git"},
			ForgeKind:  "bitbucket",
		}).Resolve(t.Context(), "origin")
		require.NoError(t, err)

		assert.Same(t, bb, gotForge)
		assert.Empty(t, bb.Options.URL)
		assert.IsType(t, (*bitbucket.RepositoryID)(nil), gotRepoID)
		assert.Equal(t, "ws/repo", gotRepoID.String())
	})

	t.Run("ExplicitURLWins", func(t *testing.T) {
		bb := &bitbucket.Forge{
			Options: bitbucket.Options{
				URL: "https://bitbucket.internal.example.com",
			},
		}
		var forges forge.Registry
		forges.Register(bb)

		_, gotRepoID, err := (&remoteResolver{
			Forges:     &forges,
			Repository: remoteURLMap{"origin": "https://git.corp.com/scm/PROJ/repo.git"},
			ForgeKind:  "bitbucket",
		}).Resolve(t.Context(), "origin")
		require.NoError(t, err)

		assert.Equal(t, "https://bitbucket.internal.example.com", bb.Options.URL)
		assert.Equal(t, "PROJ/repo", gotRepoID.String())
		assert.Equal(t,
			"https://bitbucket.internal.example.com/projects/PROJ/repos/repo/pull-requests/1/overview",
			gotRepoID.ChangeURL(&bitbucket.PR{Number: 1}))
	})
}

type remoteURLMap map[string]string

func (m remoteURLMap) RemoteConfigURL(_ context.Context, remote string) (string, error) {
	return m[remote], nil
}

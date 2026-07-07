package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/git/giturl"
	"go.abhg.dev/gs/internal/silog"
	"go.uber.org/mock/gomock"
)

func TestRemoteResolver_Resolve(t *testing.T) {
	ctrl := gomock.NewController(t)

	repoID := forgetest.NewMockRepositoryID(ctrl)
	testForge := forgetest.NewMockForge(ctrl)
	testForge.EXPECT().ID().Return("test").AnyTimes()
	testForge.EXPECT().
		ParseRepositoryPath("/owner/repo").
		Return(repoID, nil)

	var forges forge.Registry
	registerTestForge(&forges, "test", testForge)

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
	testForge.EXPECT().
		ParseRepositoryPath("/owner/repo").
		Return(repoID, nil)

	var forges forge.Registry
	registerTestForge(&forges, "test", testForge)

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
	registerTestForge(&forges, "github", testForge)

	gotForge, gotRepoID, err := (&remoteResolver{
		Forges:     &forges,
		Repository: remoteURLMap{"origin": "ssh://githubaccount1/owner/repo.git"},
		ForgeKind:  "github",
	}).Resolve(t.Context(), "origin")
	require.NoError(t, err)

	assert.Same(t, testForge, gotForge)
	assert.Equal(t, repoID, gotRepoID)
}

func TestRemoteResolver_Resolve_configuredKindConstructsWithResolvedURL(t *testing.T) {
	ctrl := gomock.NewController(t)

	repoID := forgetest.NewMockRepositoryID(ctrl)
	testForge := forgetest.NewMockForge(ctrl)
	testForge.EXPECT().ID().Return("github").AnyTimes()
	testForge.EXPECT().
		ParseRepositoryPath("/owner/repo.git").
		Return(repoID, nil)

	var gotRemoteURL *giturl.URL
	var forges forge.Registry
	forges.Register(testDefinition{
		id: "github",
		new: func(remoteURL *giturl.URL) (forge.Forge, error) {
			gotRemoteURL = remoteURL
			return testForge, nil
		},
	})

	gotForge, gotRepoID, err := (&remoteResolver{
		Forges: &forges,
		Repository: remoteURLs{
			config: map[string]string{
				"origin": "ssh://githubalias/owner/repo.git",
			},
			resolved: map[string]string{
				"origin": "https://example.com/owner/repo.git",
			},
		},
		ForgeKind: "github",
	}).Resolve(t.Context(), "origin")
	require.NoError(t, err)

	assert.Same(t, testForge, gotForge)
	assert.Equal(t, repoID, gotRepoID)
	require.NotNil(t, gotRemoteURL)
	assert.Equal(t, "https://example.com/owner/repo.git", gotRemoteURL.Raw)
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

func registerTestForge(r *forge.Registry, id string, f forge.Forge) {
	r.Register(testDefinition{
		id: id,
		new: func(*giturl.URL) (forge.Forge, error) {
			return f, nil
		},
	})
}

type testDefinition struct {
	id  string
	new func(*giturl.URL) (forge.Forge, error)
}

func (d testDefinition) ID() string { return d.id }

func (testDefinition) BaseURL() string { return "https://example.com" }

func (d testDefinition) CLIPlugin() any { return nil }

func (testDefinition) MarshalChangeMetadata(forge.ChangeMetadata) (json.RawMessage, error) {
	return nil, nil
}

func (testDefinition) UnmarshalChangeMetadata(json.RawMessage) (forge.ChangeMetadata, error) {
	return nil, nil
}

func (d testDefinition) New(remoteURL *giturl.URL) (forge.Forge, error) {
	return d.new(remoteURL)
}

type remoteURLMap map[string]string

func (m remoteURLMap) RemoteConfigURL(_ context.Context, remote string) (string, error) {
	return m[remote], nil
}

func (m remoteURLMap) RemoteURL(ctx context.Context, remote string) (string, error) {
	return m.RemoteConfigURL(ctx, remote)
}

type remoteURLs struct {
	config   map[string]string
	resolved map[string]string
}

func (m remoteURLs) RemoteConfigURL(_ context.Context, remote string) (string, error) {
	return m.config[remote], nil
}

func (m remoteURLs) RemoteURL(_ context.Context, remote string) (string, error) {
	return m.resolved[remote], nil
}

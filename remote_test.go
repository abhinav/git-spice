package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
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
	}).ResolveID(t.Context(), "origin")
	require.NoError(t, err)

	assert.Equal(t, repoID, gotRepoID)
}

func TestRemoteResolver_Resolve_unsupported(t *testing.T) {
	var forges forge.Registry
	_, _, err := (&remoteResolver{
		Forges:     &forges,
		Repository: remoteURLMap{"origin": "https://example.com/owner/repo"},
	}).Resolve(t.Context(), "origin")
	require.Error(t, err)

	var unsupported *unsupportedForgeError
	require.ErrorAs(t, err, &unsupported)
	assert.Equal(t, "origin", unsupported.Remote)
	assert.Equal(t, "https://example.com/owner/repo", unsupported.RemoteURL)
}

type remoteURLMap map[string]string

func (m remoteURLMap) RemoteURL(_ context.Context, remote string) (string, error) {
	return m[remote], nil
}

package spice_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/log/logtest"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	gomock "go.uber.org/mock/gomock"
)

func TestListChangeTemplates(t *testing.T) {
	t.Parallel()

	upstream, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		git init
		git add .
		git commit -m 'Initial commit'

		-- CHANGE_TEMPLATE.md --
		change template
	`)))
	require.NoError(t, err)

	ctx := t.Context()
	repo, err := git.Clone(ctx, upstream.Dir(), t.TempDir(), git.CloneOptions{
		Log: logtest.New(t),
	})
	require.NoError(t, err)

	mockCtrl := gomock.NewController(t)
	mockForge := forgetest.NewMockForge(mockCtrl)
	mockForge.EXPECT().
		ChangeTemplatePaths().
		Return([]string{"CHANGE_TEMPLATE.md"}).
		AnyTimes()

	store := spice.NewMemoryStore(t)
	svc := spice.NewTestService(repo, store, new(forge.Registry), logtest.New(t))

	tmpl := &forge.ChangeTemplate{
		Filename: "CHANGE_TEMPLATE.md",
		Body:     "change template",
	}

	remoteRepo := forgetest.NewMockRepository(mockCtrl)
	remoteRepo.EXPECT().Forge().Return(mockForge).AnyTimes()
	remoteRepo.EXPECT().
		ListChangeTemplates(gomock.Any()).
		Return([]*forge.ChangeTemplate{tmpl}, nil)

	got, err := svc.ListChangeTemplates(ctx, "origin", remoteRepo)
	require.NoError(t, err)
	assert.Equal(t, []*forge.ChangeTemplate{tmpl}, got)

	t.Run("Cached", func(t *testing.T) {
		ctx := t.Context()
		_, cachedTemplates, err := store.LoadCachedTemplates(ctx)
		require.NoError(t, err)
		if assert.Len(t, cachedTemplates, 1) {
			assert.Equal(t, tmpl.Body, cachedTemplates[0].Body)
		}

		// Look up again, cache should be used.
		got, err := svc.ListChangeTemplates(ctx, "origin", remoteRepo)
		require.NoError(t, err)
		assert.Equal(t, []*forge.ChangeTemplate{tmpl}, got)
	})

	t.Run("Timeout", func(t *testing.T) {
		ctx := t.Context()
		// Change the cache key to force a cache miss,
		// and cause the forge to time out.
		require.NoError(t, store.CacheTemplates(ctx, "different", []*state.CachedTemplate{
			{
				Filename: ".shamhub/CHANGE_TEMPLATE.md",
				Body:     "different",
			},
		}))

		remoteRepo.EXPECT().
			ListChangeTemplates(gomock.Any()).
			Return(nil, context.DeadlineExceeded)

		got, err := svc.ListChangeTemplates(ctx, "origin", remoteRepo)
		require.NoError(t, err)

		assert.Equal(t, []*forge.ChangeTemplate{
			{
				Filename: ".shamhub/CHANGE_TEMPLATE.md",
				Body:     "different",
			},
		}, got)
	})

	t.Run("TimeoutNoCache", func(t *testing.T) {
		ctx := t.Context()
		require.NoError(t, store.CacheTemplates(ctx, "different", nil))

		remoteRepo.EXPECT().
			ListChangeTemplates(gomock.Any()).
			Return(nil, context.DeadlineExceeded)

		_, err := svc.ListChangeTemplates(ctx, "origin", remoteRepo)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

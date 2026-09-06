package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.uber.org/mock/gomock"
)

func TestRequireReviewRepository_unsupported(t *testing.T) {
	ctrl := gomock.NewController(t)
	remote := forgetest.NewMockRepository(ctrl)
	remoteForge := forgetest.NewMockForge(ctrl)

	remote.
		EXPECT().
		Forge().
		Return(remoteForge)
	remoteForge.
		EXPECT().
		ID().
		Return("test")

	got, err := requireReviewRepository(remote)
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrUnsupported)
	assert.EqualError(
		t,
		err,
		`forge "test" does not support review comments: unsupported operation`,
	)
	assert.Nil(t, got)
}

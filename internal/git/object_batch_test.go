package git_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestIntegrationReadObjects(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	firstBody := []byte("first\nline\x00tail")
	firstHash, err := repo.WriteObject(ctx, git.BlobType, bytes.NewReader(firstBody))
	require.NoError(t, err)
	secondBody := []byte("second")
	secondHash, err := repo.WriteObject(ctx, git.BlobType, bytes.NewReader(secondBody))
	require.NoError(t, err)

	var got [][]byte
	reader, err := repo.ReadObjects(ctx, []git.Hash{firstHash, secondHash})
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, reader.Close())
	}()

	for {
		obj, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		switch obj.ID {
		case firstHash:
			assert.Equal(t, git.BlobType, obj.Type)
			assert.Equal(t, int64(len(firstBody)), obj.Size)
		case secondHash:
			assert.Equal(t, git.BlobType, obj.Type)
			assert.Equal(t, int64(len(secondBody)), obj.Size)
		default:
			t.Fatalf("unexpected object ID %v", obj.ID)
		}
		assert.Len(t, obj.ID, 40)

		body, err := io.ReadAll(reader)
		require.NoError(t, err)
		got = append(got, body)
	}

	assert.Equal(t, [][]byte{firstBody, secondBody}, got)
}

func TestIntegrationReadObjects_skipsUnreadBody(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	firstHash, err := repo.WriteObject(ctx, git.BlobType, bytes.NewReader([]byte("first")))
	require.NoError(t, err)
	secondHash, err := repo.WriteObject(ctx, git.BlobType, bytes.NewReader([]byte("second")))
	require.NoError(t, err)

	var got []string
	reader, err := repo.ReadObjects(ctx, []git.Hash{firstHash, secondHash})
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, reader.Close())
	}()

	for {
		obj, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if obj.ID == firstHash {
			buf := make([]byte, 2)
			_, err = io.ReadFull(reader, buf)
			require.NoError(t, err)
			got = append(got, string(buf))
			continue
		}

		body, err := io.ReadAll(reader)
		require.NoError(t, err)
		got = append(got, string(body))
	}

	assert.Equal(t, []string{"fi", "second"}, got)
}

func TestIntegrationReadObjects_stopsBeforeReadingBody(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	firstHash, err := repo.WriteObject(ctx, git.BlobType, bytes.NewReader([]byte("first")))
	require.NoError(t, err)
	secondHash, err := repo.WriteObject(ctx, git.BlobType, bytes.NewReader([]byte("second")))
	require.NoError(t, err)

	reader, err := repo.ReadObjects(ctx, []git.Hash{firstHash, secondHash})
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, reader.Close())
	}()

	obj, err := reader.Next()
	require.NoError(t, err)

	assert.Equal(t, firstHash, obj.ID)
}

func TestIntegrationReadObjects_missingObject(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	reader, err := repo.ReadObjects(ctx, []git.Hash{git.ZeroHash})
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, reader.Close())
	}()

	_, err = reader.Next()
	require.Error(t, err)
	assert.ErrorIs(t, err, git.ErrNotExist)
}

func TestIntegrationReadObjects_closeBeforeReadingBody(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	firstHash, err := repo.WriteObject(ctx, git.BlobType, bytes.NewReader([]byte("first")))
	require.NoError(t, err)
	secondHash, err := repo.WriteObject(ctx, git.BlobType, bytes.NewReader([]byte("second")))
	require.NoError(t, err)

	reader, err := repo.ReadObjects(ctx, []git.Hash{firstHash, secondHash})
	require.NoError(t, err)

	obj, err := reader.Next()
	require.NoError(t, err)
	assert.Equal(t, firstHash, obj.ID)

	assert.NoError(t, reader.Close())
}

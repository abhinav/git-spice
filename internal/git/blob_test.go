package git_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
	"pgregory.net/rapid"
)

func TestIntegrationWriteBlobs(t *testing.T) {
	t.Parallel()
	repo, _, err := git.Init(t.Context(), t.TempDir(), git.InitOptions{Log: silogtest.New(t)})
	require.NoError(t, err)
	bodies := [][]byte{
		nil,
		[]byte("one\nline\x00tail"),
		bytes.Repeat([]byte("binary\x00\n"), 20000),
		[]byte("short"),
		[]byte("one\nline\x00tail"),
	}
	var want []git.Hash
	for _, body := range bodies {
		object := fmt.Appendf(nil, "blob %d\x00", len(body))
		object = append(object, body...)
		want = append(want, git.Hash(fmt.Sprintf("%x", sha1.Sum(object))))
	}
	got, err := sliceutil.CollectErr(repo.WriteBlobs(t.Context(), func(yield func([]byte, error) bool) {
		for _, body := range bodies {
			if !yield(body, nil) {
				return
			}
		}
	}))
	require.NoError(t, err)
	assert.Equal(t, want, got)
	for i, hash := range got {
		var body bytes.Buffer
		require.NoError(t, repo.ReadObject(t.Context(), git.BlobType, hash, &body))
		assert.True(t, bytes.Equal(bodies[i], body.Bytes()), "object %d", i)
	}
}

func TestIntegrationWriteBlobs_roundTrip(t *testing.T) {
	t.Parallel()
	repo, _, err := git.Init(t.Context(), t.TempDir(), git.InitOptions{Log: silogtest.New(t)})
	require.NoError(t, err)
	rapid.Check(t, func(t *rapid.T) {
		bodies := rapid.SliceOfN(rapid.SliceOfN(rapid.Byte(), 0, 4096), 0, 16).
			Draw(t, "bodies")
		hashes, err := sliceutil.CollectErr(repo.WriteBlobs(t.Context(), func(yield func([]byte, error) bool) {
			for _, body := range bodies {
				if !yield(body, nil) {
					return
				}
			}
		}))
		require.NoError(t, err)
		require.Len(t, hashes, len(bodies))
		reader, err := repo.ReadObjects(t.Context(), hashes)
		require.NoError(t, err)
		defer func() { assert.NoError(t, reader.Close()) }()
		for i, hash := range hashes {
			info, err := reader.Next()
			require.NoError(t, err)
			assert.Equal(t, hash, info.ID)
			body, err := io.ReadAll(reader)
			require.NoError(t, err)
			assert.True(t, bytes.Equal(bodies[i], body), "object %d", i)
		}
		_, err = reader.Next()
		assert.ErrorIs(t, err, io.EOF)
		require.NoError(t, reader.Close())
	})
}

func TestIntegrationWriteBlobs_producerError(t *testing.T) {
	t.Parallel()
	repo, _, err := git.Init(t.Context(), t.TempDir(), git.InitOptions{Log: silogtest.New(t)})
	require.NoError(t, err)
	wantErr := errors.New("serialize blob")
	for _, count := range []int{0, 1, 2, 3} {
		t.Run(fmt.Sprintf("After%d", count), func(t *testing.T) {
			var stopped bool
			var gotErr error
			for hash, err := range repo.WriteBlobs(t.Context(), func(yield func([]byte, error) bool) {
				defer func() { stopped = true }()
				for range count {
					if !yield([]byte("body"), nil) {
						return
					}
				}
				yield(nil, wantErr)
			}) {
				if err != nil {
					assert.NoError(t, gotErr, "only one terminal error")
					gotErr = err
					continue
				}
				require.NoError(t, gotErr, "no hashes after an error")
				var body bytes.Buffer
				require.NoError(t, repo.ReadObject(t.Context(), git.BlobType, hash, &body))
				assert.Equal(t, "body", body.String())
			}
			assert.ErrorIs(t, gotErr, wantErr)
			assert.True(t, stopped)
		})
	}
}

func TestIntegrationWriteBlobs_cancel(t *testing.T) {
	t.Parallel()
	repo, _, err := git.Init(t.Context(), t.TempDir(), git.InitOptions{Log: silogtest.New(t)})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var gotErr error
	for _, err := range repo.WriteBlobs(ctx, func(yield func([]byte, error) bool) {
		for i := range 10 {
			if i == 3 {
				cancel()
			}
			if !yield([]byte("body"), nil) {
				return
			}
		}
	}) {
		if err != nil {
			gotErr = err
		}
	}
	assert.Error(t, gotErr)
}

func TestIntegrationWriteBlobs_partialResults(t *testing.T) {
	t.Parallel()
	repo, _, err := git.Init(t.Context(), t.TempDir(), git.InitOptions{Log: silogtest.New(t)})
	require.NoError(t, err)
	wantErr := errors.New("producer failed after an acknowledgement")
	var hashes []git.Hash
	var gotErr error
	for hash, err := range repo.WriteBlobs(t.Context(), func(yield func([]byte, error) bool) {
		for i := range 100 {
			if len(hashes) > 0 {
				break
			}
			if !yield(fmt.Appendf(nil, "body %d", i), nil) {
				return
			}
		}
		yield(nil, wantErr)
	}) {
		if err != nil {
			assert.NoError(t, gotErr, "only one terminal error")
			gotErr = err
			continue
		}
		require.NoError(t, gotErr, "no hashes after an error")
		hashes = append(hashes, hash)
	}
	require.ErrorIs(t, gotErr, wantErr)
	require.NotEmpty(t, hashes)
	for i, hash := range hashes {
		var body bytes.Buffer
		require.NoError(t, repo.ReadObject(t.Context(), git.BlobType, hash, &body))
		assert.Equal(t, fmt.Sprintf("body %d", i), body.String())
	}
}

func TestIntegrationWriteBlobs_stopEarly(t *testing.T) {
	t.Parallel()
	repo, _, err := git.Init(t.Context(), t.TempDir(), git.InitOptions{Log: silogtest.New(t)})
	require.NoError(t, err)
	var stopped bool
	var produced int
	var first git.Hash
	for hash, err := range repo.WriteBlobs(t.Context(), func(yield func([]byte, error) bool) {
		defer func() { stopped = true }()
		for i := range 100 {
			produced++
			if !yield(fmt.Appendf(nil, "body %d", i), nil) {
				return
			}
		}
	}) {
		require.NoError(t, err)
		first = hash
		break
	}
	assert.True(t, stopped)
	assert.Less(t, produced, 100, "stopping must not consume the remaining input")
	var body bytes.Buffer
	require.NoError(t, repo.ReadObject(t.Context(), git.BlobType, first, &body))
	assert.Equal(t, "body 0", body.String())
}

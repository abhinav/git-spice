package git

import (
	"bytes"
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestWriteBlobs_processFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("hash-object failure")
	for _, phase := range []string{"Start", "Wait"} {
		t.Run(phase, func(t *testing.T) {
			exec := &blobFailureExecer{execer: _realExec}
			if phase == "Start" {
				exec.startErr = wantErr
			} else {
				exec.waitErr = wantErr
			}
			repo, _, err := Init(t.Context(), t.TempDir(), InitOptions{
				Log: silogtest.New(t), exec: exec,
			})
			require.NoError(t, err)
			var hashes []Hash
			var gotErr error
			for hash, err := range repo.WriteBlobs(t.Context(), func(yield func([]byte, error) bool) {
				yield([]byte("body"), nil)
			}) {
				if err != nil {
					assert.NoError(t, gotErr, "only one terminal error")
					gotErr = err
					continue
				}
				require.NoError(t, gotErr, "no hashes after an error")
				hashes = append(hashes, hash)
			}
			assert.ErrorIs(t, gotErr, wantErr)
			if phase == "Start" {
				assert.Empty(t, hashes)
			} else {
				require.Len(t, hashes, 1)
				var body bytes.Buffer
				require.NoError(t, repo.ReadObject(t.Context(), BlobType, hashes[0], &body))
				assert.Equal(t, "body", body.String())
			}
		})
	}
}

func TestWriteBlobs_stopEarlyWaits(t *testing.T) {
	t.Parallel()
	exec := &blobFailureExecer{execer: _realExec}
	repo, _, err := Init(t.Context(), t.TempDir(), InitOptions{
		Log: silogtest.New(t), exec: exec,
	})
	require.NoError(t, err)
	for _, err := range repo.WriteBlobs(t.Context(), func(yield func([]byte, error) bool) {
		for range 100 {
			if !yield([]byte("body"), nil) {
				return
			}
		}
	}) {
		require.NoError(t, err)
		break
	}
	require.NotNil(t, exec.started)
	assert.NotNil(t, exec.started.ProcessState, "the Git process must be reaped before returning")
}

type blobFailureExecer struct {
	execer

	startErr error
	waitErr  error
	started  *exec.Cmd
}

func (e *blobFailureExecer) Start(cmd *exec.Cmd) error {
	if e.startErr != nil {
		return e.startErr
	}
	e.started = cmd
	return e.execer.Start(cmd)
}

func (e *blobFailureExecer) Wait(cmd *exec.Cmd) error {
	return errors.Join(e.execer.Wait(cmd), e.waitErr)
}

package git_test

import (
	"context"
	"io"
	"os/exec"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.uber.org/mock/gomock"
)

func TestRepositoryListRemoteRefs(t *testing.T) {
	mockExecer := git.NewMockExecer(gomock.NewController(t))
	repo := git.NewTestRepository(t, "", mockExecer)
	ctx := context.Background()

	var wg sync.WaitGroup
	defer wg.Wait()

	mockExecer.EXPECT().
		Start(gomock.Any()).
		Do(func(cmd *exec.Cmd) error {
			wg.Add(1)
			go func() {
				defer wg.Done()

				_, _ = io.WriteString(cmd.Stdout, "abc123\trefs/heads/main\n")
				_, _ = io.WriteString(cmd.Stdout, "malformed entry is ignored\n")
				_, _ = io.WriteString(cmd.Stdout, "def456\trefs/heads/feature\n")
				assert.NoError(t, cmd.Stdout.(io.Closer).Close())
			}()
			return nil
		})
	mockExecer.EXPECT().
		Wait(gomock.Any()).
		Return(nil)

	got, err := sliceutil.CollectErr(repo.ListRemoteRefs(ctx, "origin", nil))
	require.NoError(t, err)

	assert.Equal(t, []git.RemoteRef{
		{
			Name: "refs/heads/main",
			Hash: "abc123",
		},
		{
			Name: "refs/heads/feature",
			Hash: "def456",
		},
	}, got)
}

func TestRepositoryListRemoteRefsOptions(t *testing.T) {
	mockExecer := git.NewMockExecer(gomock.NewController(t))
	repo := git.NewTestRepository(t, "", mockExecer)
	ctx := context.Background()

	var wg sync.WaitGroup
	defer wg.Wait()

	mockExecer.EXPECT().
		Start(gomock.Any()).
		Do(func(cmd *exec.Cmd) error {
			assert.Equal(t, []string{
				"ls-remote", "--quiet",
				"--heads", "origin", "refs/heads/feat*",
			}, cmd.Args[1:])

			wg.Add(1)
			go func() {
				defer wg.Done()

				_, _ = io.WriteString(cmd.Stdout, "abc123\trefs/heads/feat1\n")
				_, _ = io.WriteString(cmd.Stdout, "def456\trefs/heads/feat2\n")
				_, _ = io.WriteString(cmd.Stdout, "ghi789\trefs/heads/feat3\n")
				assert.NoError(t, cmd.Stdout.(io.Closer).Close())
			}()
			return nil
		})
	mockExecer.EXPECT().
		Kill(gomock.Any()).
		Return(nil)

	opts := git.ListRemoteRefsOptions{
		Heads:    true,
		Patterns: []string{"refs/heads/feat*"},
	}

	for ref, err := range repo.ListRemoteRefs(ctx, "origin", &opts) {
		require.NoError(t, err)
		assert.Equal(t, git.RemoteRef{
			Name: "refs/heads/feat1",
			Hash: "abc123",
		}, ref)
		break
	}
}

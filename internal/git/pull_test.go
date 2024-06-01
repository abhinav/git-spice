package git

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestPullArgs(t *testing.T) {
	tests := []struct {
		name string
		give PullOptions

		want []string
	}{
		{
			name: "no options",
			want: []string{"pull"},
		},
		{
			name: "rebase",
			give: PullOptions{Rebase: true},
			want: []string{"pull", "--rebase"},
		},
		{
			name: "remote",
			give: PullOptions{Remote: "origin"},
			want: []string{"pull", "origin"},
		},
		{
			name: "autostash",
			give: PullOptions{Autostash: true},
			want: []string{"pull", "--autostash"},
		},
		{
			name: "refspec",
			give: PullOptions{
				Remote:  "origin",
				Refspec: "main",
			},
			want: []string{"pull", "origin", "main"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockExecer := NewMockExecer(gomock.NewController(t))
			repo := NewTestRepository(t, "", mockExecer)

			mockExecer.EXPECT().
				Run(gomock.Any()).
				DoAndReturn(func(cmd *exec.Cmd) error {
					assert.Equal(t, tt.want, cmd.Args[1:])
					return nil
				})

			ctx := context.Background()
			err := repo.Pull(ctx, tt.give)
			require.NoError(t, err)
		})
	}
}

func TestPullErrors(t *testing.T) {
	execer := NewMockExecer(gomock.NewController(t))
	repo := NewTestRepository(t, "", execer)
	ctx := context.Background()

	t.Run("refspec without remote", func(t *testing.T) {
		if err := repo.Pull(ctx, PullOptions{Refspec: "main"}); assert.Error(t, err) {
			assert.ErrorContains(t, err, "refspec specified without remote")
		}
	})

	t.Run("git error", func(t *testing.T) {
		giveErr := errors.New("great sadness")
		execer.EXPECT().
			Run(gomock.Any()).
			Return(giveErr)

		err := repo.Pull(ctx, PullOptions{})
		require.Error(t, err)
		assert.ErrorIs(t, err, giveErr)
	})
}

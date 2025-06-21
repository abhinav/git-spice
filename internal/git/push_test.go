package git

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestPushOptions_NoVerify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    PushOptions
		wantCmd []string
	}{
		{
			name: "RefspecOnly",
			opts: PushOptions{
				Remote:  "origin",
				Refspec: "HEAD:refs/heads/main",
			},
			wantCmd: []string{"push", "origin", "HEAD:refs/heads/main"},
		},
		{
			name: "NoVerify",
			opts: PushOptions{
				Remote:   "origin",
				Refspec:  "HEAD:refs/heads/main",
				NoVerify: true,
			},
			wantCmd: []string{"push", "--no-verify", "origin", "HEAD:refs/heads/main"},
		},
		{
			name: "NoVerifyWithForce",
			opts: PushOptions{
				Remote:   "origin",
				Refspec:  "HEAD:refs/heads/main",
				Force:    true,
				NoVerify: true,
			},
			wantCmd: []string{"push", "--force", "--no-verify", "origin", "HEAD:refs/heads/main"},
		},
		{
			name: "NoVerifyWithForceWithLease",
			opts: PushOptions{
				Remote:         "origin",
				Refspec:        "HEAD:refs/heads/main",
				ForceWithLease: "main:abc123",
				NoVerify:       true,
			},
			wantCmd: []string{"push", "--force-with-lease=main:abc123", "--no-verify", "origin", "HEAD:refs/heads/main"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotCmd []string
			mockExecer := NewMockExecer(gomock.NewController(t))
			mockExecer.EXPECT().
				Run(gomock.Any()).
				DoAndReturn(func(cmd *exec.Cmd) error {
					gotCmd = cmd.Args[1:]
					return nil
				})

			repo := &Repository{
				exec: mockExecer,
			}

			err := repo.Push(t.Context(), tt.opts)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCmd, gotCmd)
		})
	}
}

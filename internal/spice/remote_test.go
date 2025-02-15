package spice

import (
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/logutil"
	gomock "go.uber.org/mock/gomock"
)

func TestUnusedBranchName(t *testing.T) {
	log := logutil.TestLogger(t)

	type listRemoteRefsCall struct {
		want []string
		give []string
	}

	tests := []struct {
		name string

		branch     string
		calls      []listRemoteRefsCall
		wantBranch string
	}{
		{
			name:   "NoExistingBranches",
			branch: "feature",
			calls: []listRemoteRefsCall{
				{
					want: []string{"feature", "feature-2", "feature-3", "feature-4", "feature-5"},
				},
			},
			wantBranch: "feature",
		},
		{
			name:   "OriginalNameTaken",
			branch: "feature",
			calls: []listRemoteRefsCall{
				{
					want: []string{"feature", "feature-2", "feature-3", "feature-4", "feature-5"},
					give: []string{"feature"},
				},
			},
			wantBranch: "feature-2",
		},
		{
			name:   "SecondBatch",
			branch: "feature",
			calls: []listRemoteRefsCall{
				{
					want: []string{"feature", "feature-2", "feature-3", "feature-4", "feature-5"},
					give: []string{"feature", "feature-2", "feature-3", "feature-4", "feature-5"},
				},
				{
					want: []string{"feature-6", "feature-7", "feature-8", "feature-9", "feature-10"},
					give: []string{"feature-6", "feature-7", "feature-8", "feature-9"},
				},
			},
			wantBranch: "feature-10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			repo := NewMockGitRepository(mockCtrl)
			svc := NewTestService(repo, NewMockStore(mockCtrl), nil, log)

			var lastCall *gomock.Call
			for _, call := range tt.calls {
				currCall := repo.EXPECT().
					ListRemoteRefs(gomock.Any(), "origin", &git.ListRemoteRefsOptions{
						Heads:    true,
						Patterns: call.want,
					}).
					Return(iterRefs(call.give...))
				if lastCall != nil {
					currCall.After(lastCall)
				}
				lastCall = currCall
			}

			got, err := svc.UnusedBranchName(t.Context(), "origin", tt.branch)
			require.NoError(t, err)
			assert.Equal(t, tt.wantBranch, got)
		})
	}
}

func TestUnusedBranchName_listError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	repo := NewMockGitRepository(mockCtrl)
	svc := NewTestService(repo, NewMockStore(mockCtrl), nil, logutil.TestLogger(t))

	repo.EXPECT().
		ListRemoteRefs(gomock.Any(), "origin", gomock.Any()).
		Return(func(yield func(git.RemoteRef, error) bool) {
			yield(git.RemoteRef{}, assert.AnError)
		})

	_, err := svc.UnusedBranchName(t.Context(), "origin", "feature")
	require.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
}

func iterRefs(branchNames ...string) iter.Seq2[git.RemoteRef, error] {
	return func(yield func(git.RemoteRef, error) bool) {
		for _, name := range branchNames {
			ref := git.RemoteRef{
				Name: "refs/heads/" + name,
				Hash: "abc123",
			}
			if !yield(ref, nil) {
				return
			}
		}
	}
}

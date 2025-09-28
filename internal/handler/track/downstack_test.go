package track

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.uber.org/mock/gomock"
)

func TestDownstackDiscoverer_Discover(t *testing.T) {
	t.Run("OnTrunk", func(t *testing.T) {
		ctx := t.Context()
		ctrl := gomock.NewController(t)

		// Setup: feature -> [commits] -> main
		// No branches between feature and main
		mainHash := git.Hash("hash-main")
		featureHash := git.Hash("hash-feature")

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), nil).
			Return(sliceutil.All2[error]([]git.LocalBranch{
				{Name: "main", Hash: mainHash},
				{Name: "feature", Hash: featureHash},
			}))

		// ListCommits excludes trunk, so only returns feature's commit
		// and any intermediate commits with no branches
		mockRepo.EXPECT().
			ListCommits(gomock.Any(), gomock.Any()).
			Return(sliceutil.All2[error]([]git.Hash{
				featureHash,
			}))

		discoverer, err := newDownstackDiscoverer(
			ctx,
			silog.Nop(),
			mockRepo,
			"main",
			NewMockDownstackDiscoveryInteraction(ctrl),
			make(map[string]struct{}),
		)
		require.NoError(t, err)

		result, err := discoverer.Discover(ctx, "feature")
		require.NoError(t, err)

		assert.Equal(t, []branchToTrack{
			{name: "feature", base: "main", baseHash: mainHash},
		}, result)
	})

	t.Run("SingleBranchDownstack", func(t *testing.T) {
		ctx := t.Context()
		ctrl := gomock.NewController(t)

		// Setup: feature2 -> feature1 -> main (at mainHash)
		mainHash := git.Hash("hash-main")
		feat1Hash := git.Hash("hash-feat1")
		feat2Hash := git.Hash("hash-feat2")

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), nil).
			Return(sliceutil.All2[error]([]git.LocalBranch{
				{Name: "main", Hash: mainHash},
				{Name: "feature1", Hash: feat1Hash},
				{Name: "feature2", Hash: feat2Hash},
			}))

		mockRepo.EXPECT().
			ListCommits(gomock.Any(), gomock.Any()).
			Return(sliceutil.All2[error]([]git.Hash{
				feat2Hash,
				feat1Hash,
			}))

		mockInteract := NewMockDownstackDiscoveryInteraction(ctrl)
		mockInteract.EXPECT().
			SelectBaseBranch("feature2", feat1Hash, []string{"feature1"}, nil).
			Return("feature1", nil)

		discoverer, err := newDownstackDiscoverer(
			ctx, silog.Nop(), mockRepo, "main", mockInteract, nil,
		)
		require.NoError(t, err)

		result, err := discoverer.Discover(ctx, "feature2")
		require.NoError(t, err)

		assert.Equal(t, []branchToTrack{
			{name: "feature2", base: "feature1", baseHash: feat1Hash},
			{name: "feature1", base: "main", baseHash: mainHash},
		}, result)
	})

	t.Run("MultipleBranchesLinear", func(t *testing.T) {
		ctx := t.Context()
		ctrl := gomock.NewController(t)

		// Setup: d -> c -> b -> a -> main
		mainHash := git.Hash("hash-main")
		aHash := git.Hash("hash-a")
		bHash := git.Hash("hash-b")
		cHash := git.Hash("hash-c")
		dHash := git.Hash("hash-d")

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), nil).
			Return(sliceutil.All2[error]([]git.LocalBranch{
				{Name: "main", Hash: mainHash},
				{Name: "a", Hash: aHash},
				{Name: "b", Hash: bHash},
				{Name: "c", Hash: cHash},
				{Name: "d", Hash: dHash},
			}))

		mockRepo.EXPECT().
			ListCommits(gomock.Any(), gomock.Any()).
			Return(sliceutil.All2[error]([]git.Hash{
				dHash,
				cHash,
				bHash,
				aHash,
			}))

		mockInteract := NewMockDownstackDiscoveryInteraction(ctrl)
		mockInteract.EXPECT().
			SelectBaseBranch("d", cHash, []string{"c"}, nil).
			Return("c", nil)
		mockInteract.EXPECT().
			SelectBaseBranch("c", bHash, []string{"b"}, nil).
			Return("b", nil)
		mockInteract.EXPECT().
			SelectBaseBranch("b", aHash, []string{"a"}, nil).
			Return("a", nil)

		discoverer, err := newDownstackDiscoverer(
			ctx, silog.Nop(), mockRepo, "main", mockInteract, nil,
		)
		require.NoError(t, err)

		result, err := discoverer.Discover(ctx, "d")
		require.NoError(t, err)

		assert.Equal(t, []branchToTrack{
			{name: "d", base: "c", baseHash: cHash},
			{name: "c", base: "b", baseHash: bHash},
			{name: "b", base: "a", baseHash: aHash},
			{name: "a", base: "main", baseHash: mainHash},
		}, result)
	})

	t.Run("MultipleBranchesAtCommitSelectOne", func(t *testing.T) {
		ctx := t.Context()
		ctrl := gomock.NewController(t)

		// Setup: feature -> [commits] -> commit with [branch1, branch2] -> main
		// User selects branch1, then skips
		mainHash := git.Hash("hash-main")
		sharedHash := git.Hash("hash-shared") // shared by branch1 and branch2
		featureHash := git.Hash("hash-feature")

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), nil).
			Return(sliceutil.All2[error]([]git.LocalBranch{
				{Name: "main", Hash: mainHash},
				{Name: "branch1", Hash: sharedHash},
				{Name: "branch2", Hash: sharedHash},
				{Name: "feature", Hash: featureHash},
			}))

		mockRepo.EXPECT().
			ListCommits(gomock.Any(), gomock.Any()).
			Return(sliceutil.All2[error]([]git.Hash{
				featureHash,
				sharedHash,
			}))

		mockInteract := NewMockDownstackDiscoveryInteraction(ctrl)
		mockInteract.EXPECT().
			SelectBaseBranch("feature", sharedHash, []string{"branch1", "branch2"}, nil).
			Return("branch1", nil)
		mockInteract.EXPECT().
			SelectBaseBranch("branch1", sharedHash, []string{"branch2"}, []string{"branch1"}).
			Return("", nil) // skip

		discoverer, err := newDownstackDiscoverer(
			ctx, silog.Nop(), mockRepo, "main", mockInteract, nil,
		)
		require.NoError(t, err)

		result, err := discoverer.Discover(ctx, "feature")
		require.NoError(t, err)

		assert.Equal(t, []branchToTrack{
			{name: "feature", base: "branch1", baseHash: sharedHash},
			{name: "branch1", base: "main", baseHash: mainHash},
		}, result)
	})

	t.Run("ChainedSelectionAtCommit", func(t *testing.T) {
		ctx := t.Context()
		ctrl := gomock.NewController(t)

		// Setup: top -> commit(hash) with [mid1, mid2] -> main
		// User selects mid1, then mid2
		mainHash := git.Hash("hash-main")
		sharedHash := git.Hash("hash-shared")
		topHash := git.Hash("hash-top")

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), nil).
			Return(sliceutil.All2[error]([]git.LocalBranch{
				{Name: "main", Hash: mainHash},
				{Name: "mid1", Hash: sharedHash},
				{Name: "mid2", Hash: sharedHash},
				{Name: "top", Hash: topHash},
			}))

		mockRepo.EXPECT().
			ListCommits(gomock.Any(), gomock.Any()).
			Return(sliceutil.All2[error]([]git.Hash{
				topHash,
				sharedHash,
			}))

		mockInteract := NewMockDownstackDiscoveryInteraction(ctrl)
		mockInteract.EXPECT().
			SelectBaseBranch("top", sharedHash, []string{"mid1", "mid2"}, nil).
			Return("mid1", nil)
		mockInteract.EXPECT().
			SelectBaseBranch("mid1", sharedHash, []string{"mid2"}, []string{"mid1"}).
			Return("mid2", nil)

		discoverer, err := newDownstackDiscoverer(
			ctx, silog.Nop(), mockRepo, "main", mockInteract, nil,
		)
		require.NoError(t, err)

		result, err := discoverer.Discover(ctx, "top")
		require.NoError(t, err)

		assert.Equal(t, []branchToTrack{
			{name: "top", base: "mid1", baseHash: sharedHash},
			{name: "mid1", base: "mid2", baseHash: sharedHash},
			{name: "mid2", base: "main", baseHash: mainHash},
		}, result)
	})

	t.Run("SkipAtCommit", func(t *testing.T) {
		ctx := t.Context()
		ctrl := gomock.NewController(t)

		// Setup: feature -> commit with [other1, other2] -> main
		// User skips both
		mainHash := git.Hash("hash-main")
		sharedHash := git.Hash("hash-shared")
		featureHash := git.Hash("hash-feature")

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), nil).
			Return(sliceutil.All2[error]([]git.LocalBranch{
				{Name: "main", Hash: mainHash},
				{Name: "other1", Hash: sharedHash},
				{Name: "other2", Hash: sharedHash},
				{Name: "feature", Hash: featureHash},
			}))

		mockRepo.EXPECT().
			ListCommits(gomock.Any(), gomock.Any()).
			Return(sliceutil.All2[error]([]git.Hash{
				featureHash,
				sharedHash,
			}))

		mockInteract := NewMockDownstackDiscoveryInteraction(ctrl)
		mockInteract.EXPECT().
			SelectBaseBranch("feature", sharedHash, []string{"other1", "other2"}, nil).
			Return("", nil)

		discoverer, err := newDownstackDiscoverer(
			ctx, silog.Nop(), mockRepo, "main", mockInteract, nil,
		)
		require.NoError(t, err)

		result, err := discoverer.Discover(ctx, "feature")
		require.NoError(t, err)

		assert.Equal(t, []branchToTrack{
			{name: "feature", base: "main", baseHash: mainHash},
		}, result)
	})

	t.Run("StopAtTrackedBranch", func(t *testing.T) {
		ctx := t.Context()
		ctrl := gomock.NewController(t)

		// Setup: new-feature -> tracked-feature -> main
		mainHash := git.Hash("hash-main")
		trackedHash := git.Hash("hash-0")
		untrackedHash := git.Hash("hash-1")

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), nil).
			Return(sliceutil.All2[error]([]git.LocalBranch{
				{Name: "main", Hash: mainHash},
				{Name: "tracked-feature", Hash: trackedHash},
				{Name: "new-feature", Hash: untrackedHash},
			}))

		mockRepo.EXPECT().
			ListCommits(gomock.Any(), gomock.Any()).
			Return(sliceutil.All2[error]([]git.Hash{
				untrackedHash,
				trackedHash,
			}))

		mockInteract := NewMockDownstackDiscoveryInteraction(ctrl)
		mockInteract.EXPECT().
			SelectBaseBranch("new-feature", trackedHash, []string{"tracked-feature"}, nil).
			Return("tracked-feature", nil)

		trackedBranches := map[string]struct{}{"tracked-feature": {}}
		discoverer, err := newDownstackDiscoverer(
			ctx,
			silog.Nop(),
			mockRepo,
			"main",
			mockInteract,
			trackedBranches,
		)
		require.NoError(t, err)

		result, err := discoverer.Discover(ctx, "new-feature")
		require.NoError(t, err)

		assert.Equal(t, []branchToTrack{
			{name: "new-feature", base: "tracked-feature", baseHash: trackedHash},
		}, result)
	})

	t.Run("SelectTrackedAtCommit", func(t *testing.T) {
		ctx := t.Context()
		ctrl := gomock.NewController(t)

		// Setup: new -> commit with [tracked, untracked] -> ...
		// User selects tracked (which is already tracked)
		mainHash := git.Hash("hash-main")
		sharedHash := git.Hash("hash-shared")
		newHash := git.Hash("hash-new")

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), nil).
			Return(sliceutil.All2[error]([]git.LocalBranch{
				{Name: "main", Hash: mainHash},
				{Name: "tracked", Hash: sharedHash},
				{Name: "untracked", Hash: sharedHash},
				{Name: "new", Hash: newHash},
			}))

		mockRepo.EXPECT().
			ListCommits(gomock.Any(), gomock.Any()).
			Return(sliceutil.All2[error]([]git.Hash{
				newHash,
				sharedHash,
			}))

		mockInteract := NewMockDownstackDiscoveryInteraction(ctrl)
		mockInteract.EXPECT().
			SelectBaseBranch("new", sharedHash, []string{"tracked", "untracked"}, nil).
			Return("tracked", nil)

		trackedBranches := map[string]struct{}{"tracked": {}}
		discoverer, err := newDownstackDiscoverer(
			ctx,
			silog.Nop(),
			mockRepo,
			"main",
			mockInteract,
			trackedBranches,
		)
		require.NoError(t, err)

		result, err := discoverer.Discover(ctx, "new")
		require.NoError(t, err)

		assert.Equal(t, []branchToTrack{
			{name: "new", base: "tracked", baseHash: sharedHash},
		}, result)
	})
}

func TestDownstackDiscoverer_Discover_errors(t *testing.T) {
	t.Run("BranchDoesNotExist", func(t *testing.T) {
		ctx := t.Context()
		ctrl := gomock.NewController(t)

		mainHash := git.Hash("hash-main")

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), nil).
			Return(sliceutil.All2[error]([]git.LocalBranch{
				{Name: "main", Hash: mainHash},
			}))

		discoverer, err := newDownstackDiscoverer(
			ctx,
			silog.Nop(),
			mockRepo,
			"main",
			NewMockDownstackDiscoveryInteraction(ctrl),
			nil,
		)
		require.NoError(t, err)

		_, err = discoverer.Discover(ctx, "nonexistent")
		assert.ErrorContains(t, err, "branch nonexistent does not exist")
	})
}

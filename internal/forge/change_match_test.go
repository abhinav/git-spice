package forge_test

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.uber.org/mock/gomock"
)

//go:generate mockgen -source=change_match_test.go -aux_files=go.abhg.dev/gs/internal/forge=forge.go -destination=mocks_test.go -package=forge_test -self_package=go.abhg.dev/gs/internal/forge_test -write_package_comment=false -typed -mock_names=changeMatcherRepository=MockChangeMatcherRepository

type changeMatcherRepository interface {
	forge.Repository

	MatchChangesToBranches(
		context.Context,
		[]string,
		*forge.MatchChangesToBranchesOptions,
	) []*forge.MatchChangesToBranchesResult
}

var _ changeMatcherRepository = (*MockChangeMatcherRepository)(nil)

func TestMatchChangesToBranches_optimized(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	want := []*forge.MatchChangesToBranchesResult{
		{Changes: []*forge.MatchedBranchChange{{
			ID:       forgetest.NewMockChangeID(mockCtrl),
			State:    forge.ChangeOpen,
			HeadHash: "head-one",
		}}},
		{Changes: []*forge.MatchedBranchChange{{
			ID:       forgetest.NewMockChangeID(mockCtrl),
			State:    forge.ChangeMerged,
			HeadHash: "head-two",
		}}},
	}
	opts := &forge.MatchChangesToBranchesOptions{
		PushRepository: forgetest.NewMockRepositoryID(mockCtrl),
	}
	branches := []string{"one", "two"}

	repo := NewMockChangeMatcherRepository(mockCtrl)
	repo.EXPECT().
		MatchChangesToBranches(gomock.Any(), branches, opts).
		Return(want)

	got := forge.MatchChangesToBranches(t.Context(), repo, branches, opts)

	assert.Equal(t, want, got)
}

func TestMatchChangesToBranches_fallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		previousGOMAXPROCS := runtime.GOMAXPROCS(2)
		t.Cleanup(func() { runtime.GOMAXPROCS(previousGOMAXPROCS) })

		wantErr := errors.New("unavailable")
		pushRepository := forgetest.NewMockRepositoryID(mockCtrl)
		opts := &forge.MatchChangesToBranchesOptions{
			PushRepository: pushRepository,
		}
		findOpts := forge.FindChangesOptions{
			Limit:          10,
			PushRepository: pushRepository,
		}
		changeID := forgetest.NewMockChangeID(mockCtrl)
		changes := map[string][]*forge.FindChangeItem{
			"one": {{
				ID:       changeID,
				URL:      "https://example.com/one",
				State:    forge.ChangeOpen,
				Subject:  "One",
				HeadHash: "head-one",
				BaseName: "main",
			}},
		}
		errs := map[string]error{"two": wantErr}
		started := make(chan string, 3)
		release := make(chan struct{})

		repo := forgetest.NewMockRepository(mockCtrl)
		repo.EXPECT().
			FindChangesByBranch(gomock.Any(), gomock.Any(), findOpts).
			DoAndReturn(func(
				_ context.Context,
				branch string,
				_ forge.FindChangesOptions,
			) ([]*forge.FindChangeItem, error) {
				started <- branch
				<-release
				return changes[branch], errs[branch]
			}).
			Times(3)

		resultC := make(chan []*forge.MatchChangesToBranchesResult, 1)
		go func() {
			resultC <- forge.MatchChangesToBranches(
				t.Context(),
				repo,
				[]string{"one", "two", "three"},
				opts,
			)
		}()

		synctest.Wait()
		require.Len(t, started, 2, "fallback should use bounded concurrency")
		startedBranches := []string{<-started, <-started}

		close(release)
		synctest.Wait()
		startedBranches = append(startedBranches, <-started)
		assert.ElementsMatch(t, []string{"one", "two", "three"}, startedBranches)

		got := <-resultC
		require.Len(t, got, 3)
		for _, result := range got {
			require.NotNil(t, result)
		}
		assert.Equal(t, []*forge.MatchedBranchChange{{
			ID:       changeID,
			State:    forge.ChangeOpen,
			HeadHash: "head-one",
		}}, got[0].Changes)
		assert.NoError(t, got[0].Err)
		assert.Empty(t, got[1].Changes)
		assert.ErrorIs(t, got[1].Err, wantErr)
		assert.Empty(t, got[2].Changes)
		assert.NoError(t, got[2].Err)
	})
}

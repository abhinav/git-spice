package merge

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	branchdel "go.abhg.dev/gs/internal/handler/delete"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/ui"
)

//go:generate mockgen -destination=mocks_test.go -package=merge -write_package_comment=false -typed=true . Service,Store,DeleteHandler,GitRepository

// fakeChangeID is a simple string-based ChangeID for testing.
type fakeChangeID string

func (f fakeChangeID) String() string { return string(f) }

func TestAwaitMerged_immediate(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeStatuses(
			gomock.Any(),
			[]forge.ChangeID{fakeChangeID("pr-1")},
		).
		Return(
			[]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil,
		)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	err := h.awaitMerged(t.Context(), mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	})
	require.NoError(t, err)
}

func TestAwaitMerged_afterPolling(t *testing.T) {
	ctrl := gomock.NewController(t)

	ids := []forge.ChangeID{fakeChangeID("pr-1")}
	mockRepo := forgetest.NewMockRepository(ctrl)

	// First call: still open.
	mockRepo.EXPECT().
		ChangeStatuses(gomock.Any(), ids).
		Return(
			[]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil,
		)
	// Second call: merged.
	mockRepo.EXPECT().
		ChangeStatuses(gomock.Any(), ids).
		Return(
			[]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil,
		)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	err := h.awaitMerged(t.Context(), mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	})
	require.NoError(t, err)
}

func TestAwaitChecks_passed(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeChecksStatus(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChecksPassed, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	err := h.awaitChecks(t.Context(), mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}, 30*time.Minute)
	require.NoError(t, err)
}

func TestAwaitChecks_failed(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeChecksStatus(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChecksFailed, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	err := h.awaitChecks(t.Context(), mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}, 30*time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CI checks failed")
}

func TestAwaitChecks_pendingZeroTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeChecksStatus(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChecksPending, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	// timeout=0 means fail immediately if pending.
	err := h.awaitChecks(t.Context(), mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CI checks pending")
}

func TestAwaitChecks_pendingThenPassed(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	first := mockRepo.EXPECT().
		ChangeChecksStatus(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChecksPending, nil)
	mockRepo.EXPECT().
		ChangeChecksStatus(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChecksPassed, nil).
		After(first.Call)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	err := h.pollChecks(
		t.Context(),
		mergeItem{
			branch:   "feat1",
			changeID: fakeChangeID("pr-1"),
		},
		5*time.Second,      // timeout
		1*time.Millisecond, // base delay (fast for test)
		2*time.Millisecond, // max delay
	)
	require.NoError(t, err)
}

func TestExecutePlan_retargets(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main").AnyTimes()

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")

	// Pre-check: pr-1 already targets main.
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResult("main"), nil)

	// Each merge: checks -> merge -> awaitMerged -> cleanup
	// -> retarget next (except last).
	expectMergeItem(mockForge, pr1)
	expectMergeItem(mockForge, pr2)
	expectMergeItem(mockForge, pr3)

	// Retarget pr-2 and pr-3 to main.
	mockForge.EXPECT().
		EditChange(gomock.Any(), pr2,
			forge.EditChangeOptions{Base: "main"}).
		Return(nil)
	mockForge.EXPECT().
		EditChange(gomock.Any(), pr3,
			forge.EditChangeOptions{Base: "main"}).
		Return(nil)

	mockDelete := NewMockDeleteHandler(ctrl)
	mockDelete.EXPECT().
		DeleteBranches(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(3)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), gomock.Any()).
		Return("", errors.New("not found")).
		Times(3)
	mockGit.EXPECT().
		Fetch(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(3)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		delete:    mockDelete,
		gitRepo:   mockGit,
		logBuffer: &logBuffer,
	})

	plan := []mergeItem{
		{branch: "feat1", changeID: pr1},
		{branch: "feat2", changeID: pr2},
		{branch: "feat3", changeID: pr3},
	}

	err := h.executePlan(t.Context(), plan, &Request{
		Branch: "feat3",
	})
	require.NoError(t, err)

	output := logBuffer.String()
	assert.Contains(t, output, "Merging feat1")
	assert.Contains(t, output, "Retargeting feat2 to main")
	assert.Contains(t, output, "Merging feat2")
	assert.Contains(t, output, "Retargeting feat3 to main")
	assert.Contains(t, output, "Merging feat3")
	assert.Contains(t, output, "All 3 change(s) merged")
}

func TestExecutePlan_noWait(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")

	// Pre-check: pr-1 already targets main.
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResult("main"), nil)

	// Checks and merge for each item.
	expectChecksAndMerge(mockForge, pr1)
	expectChecksAndMerge(mockForge, pr2)
	// No ChangesStates polling (awaitMerged skipped).

	// Retarget pr-2 to main (--no-wait still retargets).
	mockForge.EXPECT().
		EditChange(gomock.Any(), pr2,
			forge.EditChangeOptions{Base: "main"}).
		Return(nil)

	// Cleanup always runs.
	mockDelete := NewMockDeleteHandler(ctrl)
	mockDelete.EXPECT().
		DeleteBranches(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(2)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), gomock.Any()).
		Return("", errors.New("not found")).
		Times(2)
	mockGit.EXPECT().
		Fetch(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(2)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		delete:    mockDelete,
		gitRepo:   mockGit,
		logBuffer: &logBuffer,
	})

	plan := []mergeItem{
		{branch: "feat1", changeID: pr1},
		{branch: "feat2", changeID: pr2},
	}

	err := h.executePlan(t.Context(), plan, &Request{
		Branch: "feat2",
		NoWait: true,
	})
	require.NoError(t, err)

	output := logBuffer.String()
	assert.Contains(t, output, "Merging feat1")
	assert.Contains(t, output, "Merging feat2")
	assert.Contains(t, output, "Retargeting feat2 to main")
	assert.Contains(t, output, "Cleaning up feat1")
	assert.Contains(t, output, "Cleaning up feat2")
}

func TestExecutePlan_singleBranch(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")

	// Pre-check: pr-1 already targets main.
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResult("main"), nil)

	expectMergeItem(mockForge, pr1)

	mockDelete := NewMockDeleteHandler(ctrl)
	mockDelete.EXPECT().
		DeleteBranches(gomock.Any(), gomock.Any()).
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), gomock.Any()).
		Return("", errors.New("not found"))
	mockGit.EXPECT().
		Fetch(gomock.Any(), gomock.Any()).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		delete:    mockDelete,
		gitRepo:   mockGit,
	})

	err := h.executePlan(t.Context(), []mergeItem{
		{branch: "feat1", changeID: pr1},
	}, &Request{Branch: "feat1"})
	require.NoError(t, err)
}

func TestExecutePlan_retargetsStaleFirstItem(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")

	// Pre-check: pr-1 has stale base "feature0".
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResult("feature0"), nil)

	// Retarget pr-1 to main before merging.
	mockForge.EXPECT().
		EditChange(gomock.Any(), pr1,
			forge.EditChangeOptions{Base: "main"}).
		Return(nil)

	expectMergeItem(mockForge, pr1)

	mockDelete := NewMockDeleteHandler(ctrl)
	mockDelete.EXPECT().
		DeleteBranches(gomock.Any(), gomock.Any()).
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), gomock.Any()).
		Return("", errors.New("not found"))
	mockGit.EXPECT().
		Fetch(gomock.Any(), gomock.Any()).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		delete:    mockDelete,
		gitRepo:   mockGit,
		logBuffer: &logBuffer,
	})

	err := h.executePlan(t.Context(), []mergeItem{
		{branch: "feat1", changeID: pr1},
	}, &Request{Branch: "feat1"})
	require.NoError(t, err)

	output := logBuffer.String()
	assert.Contains(t, output, "Retargeting feat1 to main")
	assert.Contains(t, output, "Merging feat1")
}

func TestExecutePlan_firstItemAlreadyOnTrunk(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")

	// Pre-check: pr-1 already targets main.
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResult("main"), nil)

	expectMergeItem(mockForge, pr1)

	mockDelete := NewMockDeleteHandler(ctrl)
	mockDelete.EXPECT().
		DeleteBranches(gomock.Any(), gomock.Any()).
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), gomock.Any()).
		Return("", errors.New("not found"))
	mockGit.EXPECT().
		Fetch(gomock.Any(), gomock.Any()).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		delete:    mockDelete,
		gitRepo:   mockGit,
		logBuffer: &logBuffer,
	})

	err := h.executePlan(t.Context(), []mergeItem{
		{branch: "feat1", changeID: pr1},
	}, &Request{Branch: "feat1"})
	require.NoError(t, err)

	assert.NotContains(t,
		logBuffer.String(), "Retargeting")
}

func TestCleanupMerged_deletesRemoteTracking(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockDelete := NewMockDeleteHandler(ctrl)
	mockDelete.EXPECT().
		DeleteBranches(gomock.Any(), &branchdel.Request{
			Branches: []string{"feat1"},
			Force:    true,
		}).
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "origin/feat1").
		Return("abc123", nil)
	mockGit.EXPECT().
		DeleteBranch(
			gomock.Any(), "origin/feat1",
			git.BranchDeleteOptions{Remote: true},
		).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		delete:    mockDelete,
		gitRepo:   mockGit,
		logBuffer: &logBuffer,
	})

	h.cleanupMerged(t.Context(), mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	})

	assert.Contains(t,
		logBuffer.String(), "Cleaning up feat1")
}

func TestCleanupMerged_usesUpstreamBranch(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockDelete := NewMockDeleteHandler(ctrl)
	mockDelete.EXPECT().
		DeleteBranches(gomock.Any(), gomock.Any()).
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	// Uses upstream name, not local branch name.
	mockGit.EXPECT().
		PeelToCommit(
			gomock.Any(), "origin/ed/feat1",
		).
		Return("abc123", nil)
	mockGit.EXPECT().
		DeleteBranch(
			gomock.Any(), "origin/ed/feat1",
			git.BranchDeleteOptions{Remote: true},
		).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		delete:  mockDelete,
		gitRepo: mockGit,
	})

	h.cleanupMerged(t.Context(), mergeItem{
		branch:         "feat1",
		changeID:       fakeChangeID("pr-1"),
		upstreamBranch: "ed/feat1",
	})
}

func TestValidateSynced_allInSync(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat1", "feat1",
		).
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat1").
		Return(git.Hash("abc123"), nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		gitRepo: mockGit,
	})

	items := []mergeItem{
		{
			branch:         "feat1",
			upstreamBranch: "feat1",
		},
	}
	err := h.validateSynced(t.Context(), items)
	require.NoError(t, err)
	assert.Equal(t, git.Hash("abc123"), items[0].headHash)
}

func TestValidateSynced_unpushed(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat1", "feat1",
		).
		Return(2, 0, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		gitRepo: mockGit,
	})

	err := h.validateSynced(t.Context(), []mergeItem{
		{
			branch:         "feat1",
			upstreamBranch: "feat1",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "feat1 (2 unpushed)")
	assert.Contains(t, err.Error(), "gs branch submit")
	assert.Contains(t, err.Error(), "git reset --hard")
}

func TestValidateSynced_behind(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat1", "feat1",
		).
		Return(0, 3, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		gitRepo: mockGit,
	})

	err := h.validateSynced(t.Context(), []mergeItem{
		{
			branch:         "feat1",
			upstreamBranch: "feat1",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "feat1 (3 behind remote)")
	assert.Contains(t, err.Error(), "out of sync")
}

func TestValidateSynced_multiple(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat1", "feat1",
		).
		Return(1, 0, nil)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat2", "feat2",
		).
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("def456"), nil)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat3", "feat3",
		).
		Return(0, 2, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		gitRepo: mockGit,
	})

	err := h.validateSynced(t.Context(), []mergeItem{
		{branch: "feat1", upstreamBranch: "feat1"},
		{branch: "feat2", upstreamBranch: "feat2"},
		{branch: "feat3", upstreamBranch: "feat3"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "feat1 (1 unpushed)")
	assert.Contains(t, err.Error(), "feat3 (2 behind remote)")
	assert.NotContains(t, err.Error(), "feat2")
}

func TestValidateSynced_errorSkipped(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat1", "feat1",
		).
		Return(0, 0, errors.New("not found"))

	h := newTestHandler(t, ctrl, testHandlerOpts{
		gitRepo: mockGit,
	})

	err := h.validateSynced(t.Context(), []mergeItem{
		{
			branch:         "feat1",
			upstreamBranch: "feat1",
		},
	})
	require.NoError(t, err)
}

// testHandlerOpts configures a Handler for testing.
// All nil fields are filled with no-op defaults.
type testHandlerOpts struct {
	forgeRepo *forgetest.MockRepository
	store     *MockStore
	delete    *MockDeleteHandler
	gitRepo   *MockGitRepository
	logBuffer *bytes.Buffer
}

// newTestHandler builds a Handler with sensible defaults
// for any fields not provided in opts.
func newTestHandler(
	t *testing.T,
	ctrl *gomock.Controller,
	opts testHandlerOpts,
) *Handler {
	t.Helper()

	return &Handler{
		Log:              testLog(opts.logBuffer),
		View:             ui.NewFileView(io.Discard),
		Remote:           "origin",
		RemoteRepository: testForgeRepo(ctrl, opts.forgeRepo),
		Store:            testStore(ctrl, opts.store),
		Service:          NewMockService(ctrl),
		Delete:           testDelete(ctrl, opts.delete),
		Repository:       testGitRepo(ctrl, opts.gitRepo),
	}
}

func testLog(buf *bytes.Buffer) *silog.Logger {
	if buf != nil {
		return silog.New(buf, nil)
	}
	return silog.Nop()
}

func testForgeRepo(
	ctrl *gomock.Controller,
	mock *forgetest.MockRepository,
) forge.Repository {
	if mock != nil {
		return mock
	}
	return forgetest.NewMockRepository(ctrl)
}

func testStore(
	ctrl *gomock.Controller, mock *MockStore,
) Store {
	if mock != nil {
		return mock
	}
	return NewMockStore(ctrl)
}

func testDelete(
	ctrl *gomock.Controller, mock *MockDeleteHandler,
) DeleteHandler {
	if mock != nil {
		return mock
	}
	return NewMockDeleteHandler(ctrl)
}

func testGitRepo(
	ctrl *gomock.Controller, mock *MockGitRepository,
) GitRepository {
	if mock != nil {
		return mock
	}
	return NewMockGitRepository(ctrl)
}

// fakeFindResult returns a minimal FindChangeItem
// with the given base branch name.
func fakeFindResult(
	base string,
) *forge.FindChangeItem {
	return &forge.FindChangeItem{
		ID:       fakeChangeID("find-id"),
		URL:      "http://example.com/1",
		State:    forge.ChangeOpen,
		Subject:  "test change",
		HeadHash: "abc123",
		BaseName: base,
		Draft:    false,
	}
}

// expectMergeItem sets up mock expectations for a full
// merge iteration: checks passed -> merge -> awaitMerged.
func expectMergeItem(
	mockForge *forgetest.MockRepository,
	id fakeChangeID,
) {
	expectChecksAndMerge(mockForge, id)

	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(),
			[]forge.ChangeID{id}).
		Return(
			[]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil,
		)
}

// expectChecksAndMerge sets up mock expectations for
// checks passed + merge (without awaitMerged polling).
func expectChecksAndMerge(
	mockForge *forgetest.MockRepository,
	id fakeChangeID,
) {
	mockForge.EXPECT().
		ChangeChecksStatus(gomock.Any(), id).
		Return(forge.ChecksPassed, nil)

	mockForge.EXPECT().
		MergeChange(gomock.Any(), id, gomock.Any()).
		Return(nil)
}

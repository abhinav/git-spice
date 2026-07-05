package merge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/handler/sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/mergequeue"
	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/spicetest"
	"go.abhg.dev/gs/internal/spice/state/statetest"
	"go.abhg.dev/gs/internal/ui"
)

//go:generate mockgen -destination=mocks_test.go -package=merge -write_package_comment=false -typed=true . Service,Store,RestackHandler,SubmitHandler,SyncHandler,GitRepository

// fakeChangeID is a simple string-based ChangeID for testing.
type fakeChangeID string

func (f fakeChangeID) String() string { return string(f) }

func TestOptions_mergeTimeoutDefault(t *testing.T) {
	var got Options
	parser, err := kong.New(&got)
	require.NoError(t, err)

	_, err = parser.Parse(nil)
	require.NoError(t, err)

	assert.Equal(t, 2*time.Minute, got.MergeTimeout)
}

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

	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}
	progress := newLogMergeProgress(silog.Nop())
	executor := &mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,

		Service: h.Service,
		Restack: h.Restack,
		Submit:  h.Submit,
		Sync:    h.Sync,

		Progress:         progress,
		MergeRequester:   &forgeMergeRequester{Repository: h.RemoteRepository},
		ReadinessChecker: &forgeReadinessChecker{Repository: h.RemoteRepository},
		Trunk:            "main",
		ReadyTimeout:     30 * time.Minute,
		MergeTimeout:     2 * time.Minute,
		Method:           forge.MergeMethodDefault,
	}

	err := executor.awaitMerged(t.Context(), item)
	require.NoError(t, err)
}

func TestAwaitMerged_afterPolling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		item := &mergeItem{
			branch:   "feat1",
			changeID: fakeChangeID("pr-1"),
		}
		progress := newLogMergeProgress(silog.Nop())
		executor := &mergePlanExecutor{
			RemoteRepository: h.RemoteRepository,
			Repository:       h.Repository,

			Service: h.Service,
			Restack: h.Restack,
			Submit:  h.Submit,
			Sync:    h.Sync,

			Progress:         progress,
			MergeRequester:   &forgeMergeRequester{Repository: h.RemoteRepository},
			ReadinessChecker: &forgeReadinessChecker{Repository: h.RemoteRepository},
			Trunk:            "main",
			ReadyTimeout:     30 * time.Minute,
			MergeTimeout:     2 * time.Minute,
			Method:           forge.MergeMethodDefault,
		}

		err := executor.awaitMerged(t.Context(), item)
		require.NoError(t, err)
	})
}

func TestAwaitMerged_respectsMergeTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)

		ids := []forge.ChangeID{fakeChangeID("pr-1")}
		mockRepo := forgetest.NewMockRepository(ctrl)
		mockRepo.EXPECT().
			ChangeStatuses(gomock.Any(), ids).
			Return(
				[]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil,
			).
			AnyTimes()

		h := newTestHandler(t, ctrl, testHandlerOpts{
			forgeRepo: mockRepo,
			logBuffer: nil,
		})

		item := &mergeItem{
			branch:   "feat1",
			changeID: fakeChangeID("pr-1"),
		}
		progress := newLogMergeProgress(silog.Nop())
		executor := &mergePlanExecutor{
			RemoteRepository: h.RemoteRepository,
			Repository:       h.Repository,

			Service: h.Service,
			Restack: h.Restack,
			Submit:  h.Submit,
			Sync:    h.Sync,

			Progress:         progress,
			MergeRequester:   &forgeMergeRequester{Repository: h.RemoteRepository},
			ReadinessChecker: &forgeReadinessChecker{Repository: h.RemoteRepository},
			Trunk:            "main",
			ReadyTimeout:     30 * time.Minute,
			MergeTimeout:     time.Nanosecond,
			Method:           forge.MergeMethodDefault,
		}

		err := executor.awaitMerged(t.Context(), item)
		require.Error(t, err)
		assert.EqualError(t, err, "timed out waiting for merge")
	})
}

func TestAwaitMergeability_ready(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeMergeability(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}
	progress := newLogMergeProgress(silog.Nop())
	executor := &mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,

		Service: h.Service,
		Restack: h.Restack,
		Submit:  h.Submit,
		Sync:    h.Sync,

		Progress:         progress,
		MergeRequester:   &forgeMergeRequester{Repository: h.RemoteRepository},
		ReadinessChecker: &forgeReadinessChecker{Repository: h.RemoteRepository},
		Trunk:            "main",
		ReadyTimeout:     30 * time.Minute,
		MergeTimeout:     2 * time.Minute,
		Method:           forge.MergeMethodDefault,
	}

	err := executor.awaitMergeability(t.Context(), item)
	require.NoError(t, err)
}

func TestAwaitMergeability_blocked(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeMergeability(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(
			forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonChecks,
			},
			nil,
		)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}
	progress := newLogMergeProgress(silog.Nop())
	executor := &mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,

		Service: h.Service,
		Restack: h.Restack,
		Submit:  h.Submit,
		Sync:    h.Sync,

		Progress:         progress,
		MergeRequester:   &forgeMergeRequester{Repository: h.RemoteRepository},
		ReadinessChecker: &forgeReadinessChecker{Repository: h.RemoteRepository},
		Trunk:            "main",
		ReadyTimeout:     30 * time.Minute,
		MergeTimeout:     2 * time.Minute,
		Method:           forge.MergeMethodDefault,
	}

	err := executor.awaitMergeability(t.Context(), item)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked: checks")
}

func TestAwaitMergeability_waitingZeroTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeMergeability(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityWaiting, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	// A zero timeout gives the checker one readiness probe
	// and then fails if the forge still reports a waiting state.
	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}
	progress := newLogMergeProgress(silog.Nop())
	executor := &mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,

		Service: h.Service,
		Restack: h.Restack,
		Submit:  h.Submit,
		Sync:    h.Sync,

		Progress:         progress,
		MergeRequester:   &forgeMergeRequester{Repository: h.RemoteRepository},
		ReadinessChecker: &forgeReadinessChecker{Repository: h.RemoteRepository},
		Trunk:            "main",
		ReadyTimeout:     0,
		MergeTimeout:     2 * time.Minute,
		Method:           forge.MergeMethodDefault,
	}
	err := executor.awaitMergeability(t.Context(), item)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready after 0s")
}

func TestAwaitMergeability_waitingThenReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockRepo := forgetest.NewMockRepository(ctrl)
		first := mockRepo.EXPECT().
			ChangeMergeability(
				gomock.Any(), fakeChangeID("pr-1"),
			).
			Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityWaiting, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
		mockRepo.EXPECT().
			ChangeMergeability(
				gomock.Any(), fakeChangeID("pr-1"),
			).
			Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil).
			After(first.Call)

		h := newTestHandler(t, ctrl, testHandlerOpts{
			forgeRepo: mockRepo,
			logBuffer: nil,
		})

		item := &mergeItem{
			branch:   "feat1",
			changeID: fakeChangeID("pr-1"),
		}
		progress := newLogMergeProgress(silog.Nop())
		executor := &mergePlanExecutor{
			RemoteRepository: h.RemoteRepository,
			Repository:       h.Repository,

			Service: h.Service,
			Restack: h.Restack,
			Submit:  h.Submit,
			Sync:    h.Sync,

			Progress:         progress,
			MergeRequester:   &forgeMergeRequester{Repository: h.RemoteRepository},
			ReadinessChecker: &forgeReadinessChecker{Repository: h.RemoteRepository},
			Trunk:            "main",
			ReadyTimeout:     30 * time.Minute,
			MergeTimeout:     2 * time.Minute,
			Method:           forge.MergeMethodDefault,
		}

		err := executor.awaitMergeability(t.Context(), item)
		require.NoError(t, err)
	})
}

func TestAwaitMergeability_unknown(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeMergeability(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityUnknown, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}
	progress := newLogMergeProgress(silog.Nop())
	executor := &mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,

		Service: h.Service,
		Restack: h.Restack,
		Submit:  h.Submit,
		Sync:    h.Sync,

		Progress:         progress,
		MergeRequester:   &forgeMergeRequester{Repository: h.RemoteRepository},
		ReadinessChecker: &forgeReadinessChecker{Repository: h.RemoteRepository},
		Trunk:            "main",
		ReadyTimeout:     30 * time.Minute,
		MergeTimeout:     2 * time.Minute,
		Method:           forge.MergeMethodDefault,
	}

	err := executor.awaitMergeability(t.Context(), item)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown state")
}

func TestAwaitMergeability_unsupported(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeMergeability(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityUnsupported, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}
	progress := newLogMergeProgress(silog.Nop())
	executor := &mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,

		Service: h.Service,
		Restack: h.Restack,
		Submit:  h.Submit,
		Sync:    h.Sync,

		Progress:         progress,
		MergeRequester:   &forgeMergeRequester{Repository: h.RemoteRepository},
		ReadinessChecker: &forgeReadinessChecker{Repository: h.RemoteRepository},
		Trunk:            "main",
		ReadyTimeout:     30 * time.Minute,
		MergeTimeout:     2 * time.Minute,
		Method:           forge.MergeMethodDefault,
	}

	err := executor.awaitMergeability(t.Context(), item)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown state")
}

func TestAwaitMergeability_readyCommandTimeoutZeroRunsOnce(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		CommandEnvironment(gomock.Any(), fakeChangeID("pr-1")).
		Return(nil, nil)

	mockForgeForge := forgetest.NewMockForge(ctrl)
	mockForgeForge.EXPECT().
		ID().
		Return("shamhub").
		AnyTimes()
	mockRepo.EXPECT().
		Forge().
		Return(mockForgeForge).
		AnyTimes()

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
	})

	counterPath := t.TempDir() + "/counter"
	executor := &mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,

		Service: h.Service,
		Restack: h.Restack,
		Submit:  h.Submit,
		Sync:    h.Sync,

		Progress:       newLogMergeProgress(silog.Nop()),
		MergeRequester: &forgeMergeRequester{Repository: h.RemoteRepository},
		ReadinessChecker: &commandReadinessChecker{
			Runner: &commandRunner{
				Log:        silog.Nop(),
				Repository: mockRepo,
				ForgeID:    "shamhub",
				Trunk:      "main",
				Runner:     &scriptrun.Runner{Log: silog.Nop()},
			},
			Script: fmt.Sprintf("printf x >> %q\nexit 1", counterPath),
		},
		Trunk:        "main",
		ReadyTimeout: 0,
		MergeTimeout: 2 * time.Minute,
		Method:       forge.MergeMethodDefault,
	}

	err := executor.awaitMergeability(t.Context(), &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready after 0s")

	got, err := os.ReadFile(counterPath)
	require.NoError(t, err)
	assert.Equal(t, "x", string(got))
}

func TestAwaitMergeability_readyCommandTimeoutBoundsFirstRun(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		CommandEnvironment(gomock.Any(), fakeChangeID("pr-1")).
		Return(nil, nil).
		AnyTimes()

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
	})

	executor := &mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,

		Service: h.Service,
		Restack: h.Restack,
		Submit:  h.Submit,
		Sync:    h.Sync,

		Progress:       newLogMergeProgress(silog.Nop()),
		MergeRequester: &forgeMergeRequester{Repository: h.RemoteRepository},
		ReadinessChecker: &commandReadinessChecker{
			Runner: &commandRunner{
				Log:        silog.Nop(),
				Repository: mockRepo,
				ForgeID:    "shamhub",
				Trunk:      "main",
				Runner:     &scriptrun.Runner{Log: silog.Nop()},
			},
			Script: "sleep 1\nexit 1",
		},
		Trunk:        "main",
		ReadyTimeout: 30 * time.Minute,
		MergeTimeout: 2 * time.Minute,
		Method:       forge.MergeMethodDefault,
	}

	start := time.Now()
	err := executor.awaitMergeabilityWithDelay(
		t.Context(),
		&mergeItem{
			branch:   "feat1",
			changeID: fakeChangeID("pr-1"),
		},
		50*time.Millisecond,
		time.Millisecond,
		time.Millisecond,
	)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready after 50ms")
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestAwaitMergeability_readyCommandTimeoutCancelsSlowPoll(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		CommandEnvironment(gomock.Any(), fakeChangeID("pr-1")).
		Return(nil, nil).
		AnyTimes()

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
	})

	counterPath := t.TempDir() + "/readiness-attempt"
	executor := &mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,

		Service: h.Service,
		Restack: h.Restack,
		Submit:  h.Submit,
		Sync:    h.Sync,

		Progress:       newLogMergeProgress(silog.Nop()),
		MergeRequester: &forgeMergeRequester{Repository: h.RemoteRepository},
		ReadinessChecker: &commandReadinessChecker{
			Runner: &commandRunner{
				Log:        silog.Nop(),
				Repository: mockRepo,
				ForgeID:    "shamhub",
				Trunk:      "main",
				Runner:     &scriptrun.Runner{Log: silog.Nop()},
			},
			Script: fmt.Sprintf(`
	if [ ! -f %[1]q ]; then
		touch %[1]q
		exit 1
	fi
	sleep 1
	exit 0
	`, counterPath),
		},
		Trunk:        "main",
		ReadyTimeout: 30 * time.Minute,
		MergeTimeout: 2 * time.Minute,
		Method:       forge.MergeMethodDefault,
	}

	err := executor.awaitMergeabilityWithDelay(
		t.Context(),
		&mergeItem{
			branch:   "feat1",
			changeID: fakeChangeID("pr-1"),
		},
		50*time.Millisecond,
		time.Millisecond,
		time.Millisecond,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready after 50ms")
	assert.NotContains(t, err.Error(), "readiness command exited with status -1")
}

func TestCommandMergeReadinessChecker_exitCodes(t *testing.T) {
	tests := []struct {
		name     string
		giveExit int
		want     forge.ChangeMergeability
		wantErr  string
	}{
		{
			name:     "Ready",
			giveExit: 0,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityReady,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:     "Waiting",
			giveExit: 1,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityWaiting,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:     "Blocked",
			giveExit: 2,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:     "UnexpectedFailure",
			giveExit: 3,
			wantErr:  "readiness command exited with status 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			mockRepo := forgetest.NewMockRepository(ctrl)
			mockRepo.EXPECT().
				CommandEnvironment(gomock.Any(), fakeChangeID("pr-1")).
				Return(nil, nil)

			check := &commandReadinessChecker{
				Runner: &commandRunner{
					Log:        silog.Nop(),
					Repository: mockRepo,
					ForgeID:    "shamhub",
					Trunk:      "main",
					Runner:     &scriptrun.Runner{Log: silog.Nop()},
				},
				Script: fmt.Sprintf("exit %d", tt.giveExit),
			}
			got, err := check.CheckMergeItemReady(t.Context(), &mergeItem{
				branch:   "feat1",
				changeID: fakeChangeID("pr-1"),
			})
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCommandMergeReadinessChecker_commandInfrastructureError(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		CommandEnvironment(gomock.Any(), fakeChangeID("pr-1")).
		Return(nil, nil)

	check := &commandReadinessChecker{
		Runner: &commandRunner{
			Log:        silog.Nop(),
			Repository: mockRepo,
			ForgeID:    "shamhub",
			Trunk:      "main",
			Runner:     &scriptrun.Runner{Log: silog.Nop()},
		},
		Script: "#!/no/such/interpreter\n",
	}
	_, err := check.CheckMergeItemReady(t.Context(), &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "run readiness command")
}

func TestCommandMergeReadinessChecker_environment(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		CommandEnvironment(gomock.Any(), fakeChangeID("pr-1")).
		Return(map[string]string{
			"GIT_SPICE_SHAMHUB_CHANGE_NUMBER": "1",
			"GIT_SPICE_BRANCH":                "wrong",
		}, nil)

	log := silog.New(&logBuffer, nil)
	check := &commandReadinessChecker{
		Runner: &commandRunner{
			Log:        log,
			Repository: mockRepo,
			ForgeID:    "shamhub",
			Trunk:      "main",
			Runner:     &scriptrun.Runner{Log: log},
		},
		Script: strings.Join([]string{
			"test \"$GIT_SPICE_FORGE_ID\" = shamhub",
			"test \"$GIT_SPICE_BRANCH\" = feat1",
			"test \"$GIT_SPICE_BASE_BRANCH\" = main",
			"test \"$GIT_SPICE_TRUNK_BRANCH\" = main",
			"test \"$GIT_SPICE_CHANGE_URL\" = http://example.com/1",
			"test \"$GIT_SPICE_HEAD_SHA\" = head1",
			"test \"$GIT_SPICE_SHAMHUB_CHANGE_NUMBER\" = 1",
			"echo readiness stdout",
			"echo readiness stderr >&2",
		}, "\n"),
	}
	got, err := check.CheckMergeItemReady(t.Context(), &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
		headHash: git.Hash("head1"),
		mergeURL: "http://example.com/1",
		base:     "main",
	})
	require.NoError(t, err)
	assert.Equal(t, forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, got)
	assert.Contains(t, logBuffer.String(), "INF merge: readiness stdout")
	assert.Contains(t, logBuffer.String(), "INF merge: readiness stderr")
}

func TestExecutePlan_retargets(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockForge := forgetest.NewMockRepository(ctrl)
	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(&spice.BranchNeedsRestackError{Base: "main"})
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat3").
		Return(&spice.BranchNeedsRestackError{Base: "main"})

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")

	// Each merge: merge readiness -> merge -> awaitMerged -> sync
	// -> prepare next (except last).
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head1")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)
	status := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{
			State:    forge.ChangeOpen,
			HeadHash: git.Hash("head2"),
		}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityReady,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil).
		After(status.Call)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr2, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)
	status = mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{
			State:    forge.ChangeOpen,
			HeadHash: git.Hash("head3"),
		}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr3).
		Return(forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityReady,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil).
		After(status.Call)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr3, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockRestack := NewMockRestackHandler(ctrl)
	mockRestack.EXPECT().
		RestackBranch(gomock.Any(), &restack.BranchRequest{
			Branch: "feat2",
		}).
		Return(nil)
	mockRestack.EXPECT().
		RestackBranch(gomock.Any(), &restack.BranchRequest{
			Branch: "feat3",
		}).
		Return(nil)

	mockSubmit := NewMockSubmitHandler(ctrl)
	mockSubmit.EXPECT().
		Submit(gomock.Any(), gomock.Any()).
		DoAndReturn(assertSubmitUpdate(t, "feat2"))
	mockSubmit.EXPECT().
		Submit(gomock.Any(), gomock.Any()).
		DoAndReturn(assertSubmitUpdate(t, "feat3"))

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat3").
		Return(git.Hash("head3"), nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), &sync.TrunkOptions{ClosedChanges: sync.ClosedChangesIgnore}).
		Return(nil).
		Times(3)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
		restack:   mockRestack,
		submit:    mockSubmit,
		sync:      mockSync,
		logBuffer: &logBuffer,
	})

	plan := []*mergeItem{
		{
			branch:   "feat1",
			base:     "main",
			changeID: pr1,
			mergeURL: testRepositoryID{}.ChangeURL(pr1),
		},
		{
			branch:   "feat2",
			base:     "feat1",
			changeID: pr2,
			mergeURL: testRepositoryID{}.ChangeURL(pr2),
		},
		{
			branch:   "feat3",
			base:     "feat2",
			changeID: pr3,
			mergeURL: testRepositoryID{}.ChangeURL(pr3),
		},
	}

	err := h.executePlan(t.Context(), plan, mergeExecutionOptions{})
	require.NoError(t, err)

	output := logBuffer.String()
	assert.Contains(t, output, "feat1: merging pr-1: http://example.com/1")
	assert.Contains(t, output, "feat2: merging pr-2: http://example.com/1")
	assert.Contains(t, output, "feat3: merging pr-3: http://example.com/1")
	assert.Contains(t, output, "All 3 change(s) merged")
	assert.NotContains(t, output, "Restacking feat2 after merge")
	assert.NotContains(t, output, "Restacking feat3 after merge")
}

func TestExecutePlan_waitsForPreparedChangeHeadBeforeChecks(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(&spice.BranchNeedsRestackError{Base: "main"})

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head1")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockRestack := NewMockRestackHandler(ctrl)
	mockRestack.EXPECT().
		RestackBranch(gomock.Any(), &restack.BranchRequest{
			Branch: "feat2",
		}).
		Return(nil)

	mockSubmit := NewMockSubmitHandler(ctrl)
	mockSubmit.EXPECT().
		Submit(gomock.Any(), gomock.Any()).
		DoAndReturn(assertSubmitUpdate(t, "feat2"))

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("new-head2"), nil)

	// The submit call can return before the forge's change view catches up
	// to the pushed branch head.
	// A stale merge readiness value at this point belongs to the old head,
	// so the merge loop must wait until the forge reports new-head2
	// before asking whether the change is ready to merge.
	status := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{
			State:    forge.ChangeOpen,
			HeadHash: git.Hash("new-head2"),
		}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil).
		After(status.Call)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr2, forge.MergeChangeOptions{
			Method:   forge.MergeMethodDefault,
			HeadHash: git.Hash("new-head2"),
		}).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), &sync.TrunkOptions{ClosedChanges: sync.ClosedChangesIgnore}).
		Return(nil).
		Times(2)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
		restack:   mockRestack,
		submit:    mockSubmit,
		sync:      mockSync,
	})

	err := h.executePlan(t.Context(), []*mergeItem{
		{
			branch:   "feat1",
			base:     "main",
			changeID: pr1,
			headHash: git.Hash("head1"),
			mergeURL: testRepositoryID{}.ChangeURL(pr1),
		},
		{
			branch:   "feat2",
			base:     "feat1",
			changeID: pr2,
			headHash: git.Hash("old-head2"),
			mergeURL: testRepositoryID{}.ChangeURL(pr2),
		},
	}, mergeExecutionOptions{})
	require.NoError(t, err)
}

func TestExecutePlan_singleBranch(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")

	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head1")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), &sync.TrunkOptions{ClosedChanges: sync.ClosedChangesIgnore}).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		sync:      mockSync,
	})

	err := h.executePlan(t.Context(), []*mergeItem{{
		branch:   "feat1",
		base:     "main",
		changeID: pr1,
		mergeURL: testRepositoryID{}.ChangeURL(pr1),
	}}, mergeExecutionOptions{})
	require.NoError(t, err)
}

func TestMergeBranch_delegatesToDownstackMerge(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head1")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat1", "feat1",
		).
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat1").
		Return(git.Hash("head1"), nil)

	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{Trunk: "main", Branches: []spice.LoadBranchItem{
		{Name: "feat1", Base: "main", Change: testChangeMetadata(pr1), UpstreamBranch: "feat1"},
	}})
	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), &sync.TrunkOptions{ClosedChanges: sync.ClosedChangesIgnore}).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
		sync:      mockSync,
	})

	err := h.MergeBranch(t.Context(), &BranchMergeRequest{
		Branches: []string{"feat1"},
	})
	require.NoError(t, err)
}

func TestMergeBranch_acceptsMultipleBranches(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
		}, nil)

	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{Trunk: "main", Branches: []spice.LoadBranchItem{
		{Name: "feat1", Base: "main", Change: testChangeMetadata(pr1)},
		{Name: "feat2", Base: "main", Change: testChangeMetadata(pr2)},
	}})

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
	})

	plan, err := h.buildPlanFromBranches(t.Context(), mergePlanRequest{
		Graph:    graph,
		Branches: []string{"feat1", "feat2", "feat1"},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"feat1", "feat2"}, mergePlanBranches(plan))
}

func TestMergeBranch_rejectsSelectedBranchWithoutBase(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
			Trunk: "main",
			Branches: []spice.LoadBranchItem{
				{
					Name:   "feat1",
					Base:   "main",
					Change: testChangeMetadata(fakeChangeID("pr-1")),
				},
				{
					Name:   "feat2",
					Base:   "feat1",
					Change: testChangeMetadata(fakeChangeID("pr-2")),
				},
				{
					Name:   "feat3",
					Base:   "feat2",
					Change: testChangeMetadata(fakeChangeID("pr-3")),
				},
			},
		}), nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		service: mockService,
	})
	err := h.MergeBranch(t.Context(), &BranchMergeRequest{
		Branches: []string{"feat2"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(),
		`branch "feat2" requires selected base "feat1"`)
}

func TestMergeBranch_rejectsSelectedBranchesWithoutPathToTrunk(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
			Trunk: "main",
			Branches: []spice.LoadBranchItem{
				{
					Name:   "feat1",
					Base:   "main",
					Change: testChangeMetadata(fakeChangeID("pr-1")),
				},
				{
					Name:   "feat2",
					Base:   "feat1",
					Change: testChangeMetadata(fakeChangeID("pr-2")),
				},
				{
					Name:   "feat3",
					Base:   "feat2",
					Change: testChangeMetadata(fakeChangeID("pr-3")),
				},
			},
		}), nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		service: mockService,
	})
	err := h.MergeBranch(t.Context(), &BranchMergeRequest{
		Branches: []string{"feat3", "feat2"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(),
		`requires selected base "feat1"`)
}

func TestMergeBranch_acceptsSelectedPathToTrunk(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	planStatus := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
		}, nil)
	staleBaseStatus := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil).
		After(planStatus.Call)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil).
		After(staleBaseStatus)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, forge.MergeChangeOptions{}).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return(
			[]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil,
		)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head2")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr2, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{Trunk: "main", Branches: []spice.LoadBranchItem{
			{Name: "feat1", Base: "main", Change: testChangeMetadata(pr1)},
			{Name: "feat2", Base: "feat1", Change: testChangeMetadata(pr2)},
		}}), nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
	})

	err := h.MergeBranch(t.Context(), &BranchMergeRequest{
		Branches: []string{"feat1", "feat2"},
	})
	require.NoError(t, err)
}

func TestMergeBranch_passesFailFastToScheduler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)

		pr1 := fakeChangeID("pr-1")
		pr2 := fakeChangeID("pr-2")
		pr3 := fakeChangeID("pr-3")
		graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
			Trunk: "main",
			Branches: []spice.LoadBranchItem{
				{
					Name:           "feat1",
					Base:           "main",
					Change:         testChangeMetadata(pr1),
					UpstreamBranch: "feat1",
				},
				{
					Name:           "feat2",
					Base:           "feat1",
					Change:         testChangeMetadata(pr2),
					UpstreamBranch: "feat2",
				},
				{
					Name:           "feat3",
					Base:           "feat1",
					Change:         testChangeMetadata(pr3),
					UpstreamBranch: "feat3",
				},
			},
		})

		mockForge := forgetest.NewMockRepository(ctrl)
		planStatuses := mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2, pr3}).
			Return([]forge.ChangeStatus{
				{State: forge.ChangeOpen},
				{State: forge.ChangeOpen},
				{State: forge.ChangeOpen},
			}, nil)
		staleBaseStatuses := mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
			Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil)
		staleBaseStatuses.After(planStatuses.Call)
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
			Return([]forge.ChangeStatus{{
				State:    forge.ChangeOpen,
				HeadHash: git.Hash("head1"),
			}}, nil).
			After(staleBaseStatuses.Call)
		mockForge.EXPECT().
			ChangeMergeability(gomock.Any(), pr1).
			Return(forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityReady,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			}, nil)
		mockForge.EXPECT().
			MergeChange(gomock.Any(), pr1, gomock.Any()).
			Return(nil)
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
			Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Nil()).
			Return(graph, nil)
		mockService.EXPECT().
			VerifyRestacked(gomock.Any(), "feat2").
			Return(nil)

		feat2Blocked := make(chan struct{})
		mockService.EXPECT().
			VerifyRestacked(gomock.Any(), "feat3").
			DoAndReturn(func(ctx context.Context, _ string) error {
				// Keep feat3 in preparation until feat2 reports blocked.
				// If FailFast is wired through,
				// the scheduler cancels this preparation before feat3 can merge.
				<-feat2Blocked
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(50 * time.Millisecond):
					return nil
				}
			}).
			AnyTimes()

		mockGit := NewMockGitRepository(ctrl)
		mockGit.EXPECT().
			CommitAheadBehind(gomock.Any(), "origin/feat1", "feat1").
			Return(0, 0, nil)
		mockGit.EXPECT().
			PeelToCommit(gomock.Any(), "feat1").
			Return(git.Hash("head1"), nil)
		mockGit.EXPECT().
			CommitAheadBehind(gomock.Any(), "origin/feat2", "feat2").
			Return(0, 0, nil)
		mockGit.EXPECT().
			PeelToCommit(gomock.Any(), "feat2").
			Return(git.Hash("head2"), nil).
			Times(2)
		mockGit.EXPECT().
			CommitAheadBehind(gomock.Any(), "origin/feat3", "feat3").
			Return(0, 0, nil)
		mockGit.EXPECT().
			PeelToCommit(gomock.Any(), "feat3").
			Return(git.Hash("head3"), nil)
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
			Return([]forge.ChangeStatus{{
				State:    forge.ChangeOpen,
				HeadHash: git.Hash("head2"),
			}}, nil)
		mockForge.EXPECT().
			ChangeMergeability(gomock.Any(), pr2).
			DoAndReturn(func(
				context.Context,
				forge.ChangeID,
			) (forge.ChangeMergeability, error) {
				close(feat2Blocked)
				return forge.ChangeMergeability{
					State:  forge.ChangeMergeabilityBlocked,
					Reason: forge.ChangeMergeabilityReasonUnknown,
				}, nil
			})

		h := newTestHandler(t, ctrl, testHandlerOpts{
			forgeRepo: mockForge,
			service:   mockService,
			gitRepo:   mockGit,
		})
		err := h.MergeBranch(t.Context(), &BranchMergeRequest{
			Branches: []string{"feat1", "feat2", "feat3"},
			Options: &Options{
				FailFast: true,
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "blocked")
	})
}

func TestMergeDownstack_passesFailFastToScheduler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)

		pr1 := fakeChangeID("pr-1")
		pr2 := fakeChangeID("pr-2")
		pr3 := fakeChangeID("pr-3")
		graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
			Trunk: "main",
			Branches: []spice.LoadBranchItem{
				{
					Name:           "feat1",
					Base:           "main",
					Change:         testChangeMetadata(pr1),
					UpstreamBranch: "feat1",
				},
				{
					Name:           "feat2",
					Base:           "feat1",
					Change:         testChangeMetadata(pr2),
					UpstreamBranch: "feat2",
				},
				{
					Name:           "feat3",
					Base:           "feat1",
					Change:         testChangeMetadata(pr3),
					UpstreamBranch: "feat3",
				},
			},
		})

		mockForge := forgetest.NewMockRepository(ctrl)
		planStatuses := mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2, pr3}).
			Return([]forge.ChangeStatus{
				{State: forge.ChangeOpen},
				{State: forge.ChangeOpen},
				{State: forge.ChangeOpen},
			}, nil)
		staleBaseStatuses := mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
			Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil)
		staleBaseStatuses.After(planStatuses.Call)
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
			Return([]forge.ChangeStatus{{
				State:    forge.ChangeOpen,
				HeadHash: git.Hash("head1"),
			}}, nil).
			After(staleBaseStatuses.Call)
		mockForge.EXPECT().
			ChangeMergeability(gomock.Any(), pr1).
			Return(forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityReady,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			}, nil)
		mockForge.EXPECT().
			MergeChange(gomock.Any(), pr1, gomock.Any()).
			Return(nil)
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
			Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(gomock.Any(), "feat2").
			Return(nil)

		feat2Blocked := make(chan struct{})
		mockService.EXPECT().
			VerifyRestacked(gomock.Any(), "feat3").
			DoAndReturn(func(ctx context.Context, _ string) error {
				// Keep feat3 in preparation until feat2 reports blocked.
				// If FailFast is wired through,
				// the scheduler cancels this preparation before feat3 can merge.
				<-feat2Blocked
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(50 * time.Millisecond):
					return nil
				}
			}).
			AnyTimes()

		mockGit := NewMockGitRepository(ctrl)
		mockGit.EXPECT().
			CommitAheadBehind(gomock.Any(), "origin/feat1", "feat1").
			Return(0, 0, nil)
		mockGit.EXPECT().
			PeelToCommit(gomock.Any(), "feat1").
			Return(git.Hash("head1"), nil)
		mockGit.EXPECT().
			CommitAheadBehind(gomock.Any(), "origin/feat2", "feat2").
			Return(0, 0, nil)
		mockGit.EXPECT().
			PeelToCommit(gomock.Any(), "feat2").
			Return(git.Hash("head2"), nil).
			Times(2)
		mockGit.EXPECT().
			CommitAheadBehind(gomock.Any(), "origin/feat3", "feat3").
			Return(0, 0, nil)
		mockGit.EXPECT().
			PeelToCommit(gomock.Any(), "feat3").
			Return(git.Hash("head3"), nil)
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
			Return([]forge.ChangeStatus{{
				State:    forge.ChangeOpen,
				HeadHash: git.Hash("head2"),
			}}, nil)
		mockForge.EXPECT().
			ChangeMergeability(gomock.Any(), pr2).
			DoAndReturn(func(
				context.Context,
				forge.ChangeID,
			) (forge.ChangeMergeability, error) {
				close(feat2Blocked)
				return forge.ChangeMergeability{
					State:  forge.ChangeMergeabilityBlocked,
					Reason: forge.ChangeMergeabilityReasonUnknown,
				}, nil
			})

		h := newTestHandler(t, ctrl, testHandlerOpts{
			forgeRepo: mockForge,
			service:   mockService,
			gitRepo:   mockGit,
		})
		err := h.MergeDownstack(t.Context(), &DownstackMergeRequest{
			Branches:    []string{"feat2", "feat3"},
			BranchGraph: graph,
			Options: &Options{
				FailFast: true,
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "blocked")
	})
}

func TestBuildPlan_expandsAndNormalizesDownstackBranches(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	planStatuses := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2, pr3}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
		}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
		}, nil).
		After(planStatuses.Call)

	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{Trunk: "main", Branches: []spice.LoadBranchItem{
		{Name: "feat1", Base: "main", Change: testChangeMetadata(pr1)},
		{Name: "feat2", Base: "feat1", Change: testChangeMetadata(pr2)},
		{Name: "feat3", Base: "feat1", Change: testChangeMetadata(pr3)},
	}})

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
	})
	plan, err := h.buildPlan(t.Context(), &DownstackMergeRequest{
		Branches:    []string{"feat2", "feat3", "feat2"},
		BranchGraph: graph,
	})
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"feat1", "feat2", "feat3"},
		mergePlanBranches(plan),
	)
}

func TestBuildPlan_rejectsUnknownDownstackBranch(t *testing.T) {
	ctrl := gomock.NewController(t)

	h := newTestHandler(t, ctrl, testHandlerOpts{})
	_, err := h.buildPlan(t.Context(), &DownstackMergeRequest{
		Branches: []string{"missing"},
		BranchGraph: spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
			Trunk: "main",
			Branches: []spice.LoadBranchItem{{
				Name:   "feat1",
				Base:   "main",
				Change: testChangeMetadata(fakeChangeID("pr-1")),
			}},
		}),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `branch "missing" is not tracked`)
}

func TestMergeStack_includesUpstackDescendants(t *testing.T) {
	ctrl := gomock.NewController(t)

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{Trunk: "main", Branches: []spice.LoadBranchItem{
		{Name: "feat1", Base: "main", Change: testChangeMetadata(pr1), UpstreamBranch: "feat1"},
		{Name: "feat2", Base: "feat1", Change: testChangeMetadata(pr2), UpstreamBranch: "feat2"},
		{Name: "feat3", Base: "feat1", Change: testChangeMetadata(pr3), UpstreamBranch: "feat3"},
	}})

	mockForge := forgetest.NewMockRepository(ctrl)
	planStatuses := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2, pr3}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
		}, nil)
	staleBaseStatuses := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
		}, nil)
	staleBaseStatuses.After(planStatuses.Call)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head1")}}, nil).
		After(staleBaseStatuses.Call)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat3").
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat1", "feat1").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat1").
		Return(git.Hash("head1"), nil)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat2", "feat2").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil).
		Times(2)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat3", "feat3").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat3").
		Return(git.Hash("head3"), nil).
		Times(2)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head2")}}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head3")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr2, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr3).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr3, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
	})

	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branches: []string{"feat1"},
	})
	require.NoError(t, err)
}

func TestMergeStack_normalizesContainedScopes(t *testing.T) {
	ctrl := gomock.NewController(t)

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{Trunk: "main", Branches: []spice.LoadBranchItem{
		{Name: "feat1", Base: "main", Change: testChangeMetadata(pr1)},
		{Name: "feat2", Base: "feat1", Change: testChangeMetadata(pr2)},
		{Name: "feat3", Base: "feat1", Change: testChangeMetadata(pr3)},
	}})

	mockForge := forgetest.NewMockRepository(ctrl)
	planStatuses := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2, pr3}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
		}, nil)
	staleBaseStatuses := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
		}, nil)
	staleBaseStatuses.After(planStatuses.Call)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat3").
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat3").
		Return(git.Hash("head3"), nil)

	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head2")}}, nil).
		After(staleBaseStatuses.Call)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr2, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head3")}}, nil).
		After(staleBaseStatuses.Call)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr3).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr3, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
	})
	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branches: []string{"feat1", "feat2"},
	})
	require.NoError(t, err)
}

func TestMergeStack_ignoresUnsubmittedAboveSubmitted(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	pr1 := fakeChangeID("pr-1")
	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{Trunk: "main", Branches: []spice.LoadBranchItem{
		{Name: "feat1", Base: "main", Change: testChangeMetadata(pr1), UpstreamBranch: "feat1"},
		{
			Name:           "feat2",
			Base:           "feat1",
			UpstreamBranch: "feat2",
		},
	}})

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
		}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head1")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat1", "feat1").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat1").
		Return(git.Hash("head1"), nil)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), &sync.TrunkOptions{ClosedChanges: sync.ClosedChangesIgnore}).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
		sync:      mockSync,
		logBuffer: &logBuffer,
	})

	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branches: []string{"feat1"},
	})
	require.NoError(t, err)
	assert.Contains(t, logBuffer.String(),
		"feat2: no published change request, skipping")
}

func TestMergeStack_ignoresUnsubmittedBelowSubmitted(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	pr2 := fakeChangeID("pr-2")
	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{Trunk: "main", Branches: []spice.LoadBranchItem{
		{
			Name:           "feat1",
			Base:           "main",
			UpstreamBranch: "feat1",
		},
		{Name: "feat2", Base: "feat1", Change: testChangeMetadata(pr2), UpstreamBranch: "feat2"},
	}})

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head2")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr2, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat2", "feat2").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil).
		Times(2)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), &sync.TrunkOptions{ClosedChanges: sync.ClosedChangesIgnore}).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
		sync:      mockSync,
		logBuffer: &logBuffer,
	})

	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branches: []string{"feat2"},
	})
	require.NoError(t, err)
	assert.Contains(t, logBuffer.String(),
		"feat1: no published change request, skipping")
}

func TestMergeStack_allSelectedBranchesUnsubmitted(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{Trunk: "main", Branches: []spice.LoadBranchItem{
		{
			Name:           "feat1",
			Base:           "main",
			UpstreamBranch: "feat1",
		},
		{
			Name:           "feat2",
			Base:           "feat1",
			UpstreamBranch: "feat2",
		},
	}})

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		service:   mockService,
		logBuffer: &logBuffer,
	})

	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branches: []string{"feat2"},
	})
	require.NoError(t, err)
	assert.Contains(t, logBuffer.String(),
		"feat1: no published change request, skipping")
	assert.Contains(t, logBuffer.String(),
		"feat2: no published change request, skipping")
	assert.Contains(t, logBuffer.String(), "No open changes to merge.")
}

func TestMergeStack_passesFailFastToScheduler(t *testing.T) {
	ctrl := gomock.NewController(t)

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{Trunk: "main", Branches: []spice.LoadBranchItem{
		{Name: "feat1", Base: "main", Change: testChangeMetadata(pr1), UpstreamBranch: "feat1"},
		{Name: "feat2", Base: "feat1", Change: testChangeMetadata(pr2), UpstreamBranch: "feat2"},
		{Name: "feat3", Base: "feat1", Change: testChangeMetadata(pr3), UpstreamBranch: "feat3"},
	}})

	mockForge := forgetest.NewMockRepository(ctrl)
	planStatuses := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2, pr3}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
		}, nil)
	staleBaseStatuses := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
		}, nil)
	staleBaseStatuses.After(planStatuses.Call)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head1")}}, nil).
		After(staleBaseStatuses.Call)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)
	// feat3 is an already-ready sibling when feat2 fails.
	// Fail-fast may stop scheduling before feat3 enters preparation,
	// or feat3 may already be in preparation by the time feat2 reports
	// blocked.
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat3").
		Return(nil).
		AnyTimes()

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat1", "feat1").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat1").
		Return(git.Hash("head1"), nil)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat2", "feat2").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil).
		Times(2)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat3", "feat3").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat3").
		Return(git.Hash("head3"), nil).
		AnyTimes()
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head2")}}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{
			State:    forge.ChangeOpen,
			HeadHash: git.Hash("head3"),
		}}, nil).
		AnyTimes()
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityBlocked, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr3).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil).
		AnyTimes()
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr3, gomock.Any()).
		Return(nil).
		AnyTimes()
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return(
			[]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil,
		).
		AnyTimes()

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
	})

	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branches: []string{"feat1"},
		Options: &Options{
			FailFast: true,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked")
}

func TestExecutePlan_syncTrunkFailureStopsLoop(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), &sync.TrunkOptions{ClosedChanges: sync.ClosedChangesIgnore}).
		Return(errors.New("sync failed"))

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		sync:      mockSync,
	})

	err := h.executePlan(t.Context(), []*mergeItem{{
		branch:   "feat1",
		base:     "main",
		changeID: pr1,
		mergeURL: testRepositoryID{}.ChangeURL(pr1),
	}}, mergeExecutionOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync trunk")

	var barrierErr *mergequeue.BarrierError
	assert.ErrorAs(t, err, &barrierErr)
	var itemErr *mergequeue.ItemError
	assert.False(t, errors.As(err, &itemErr))
}

func TestExecutePlan_mergeMethod(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")
	status := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head1")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil).
		After(status.Call)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, forge.MergeChangeOptions{
			Method:   forge.MergeMethodSquash,
			HeadHash: git.Hash("head1"),
		}).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), &sync.TrunkOptions{ClosedChanges: sync.ClosedChangesIgnore}).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		sync:      mockSync,
	})

	err := h.executePlan(t.Context(), []*mergeItem{
		{
			branch:   "feat1",
			base:     "main",
			changeID: pr1,
			headHash: git.Hash("head1"),
			mergeURL: testRepositoryID{}.ChangeURL(pr1),
		},
	}, mergeExecutionOptions{
		Method: forge.MergeMethodSquash,
	})
	require.NoError(t, err)
}

func TestExecutePlan_usesForgeReadinessWhenReadyCommandEmpty(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, forge.MergeChangeOptions{}).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), &sync.TrunkOptions{ClosedChanges: sync.ClosedChangesIgnore}).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		sync:      mockSync,
	})

	err := h.executePlan(t.Context(), []*mergeItem{{
		branch:   "feat1",
		base:     "main",
		changeID: pr1,
		mergeURL: testRepositoryID{}.ChangeURL(pr1),
	}}, mergeExecutionOptions{})
	require.NoError(t, err)
}

func TestExecutePlan_readyCommandReplacesForgeReadiness(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForgeForge := forgetest.NewMockForge(ctrl)
	mockForgeForge.EXPECT().
		ID().
		Return("shamhub").
		AnyTimes()
	mockForge.EXPECT().
		Forge().
		Return(mockForgeForge).
		AnyTimes()

	pr1 := fakeChangeID("pr-1")
	mockForge.EXPECT().
		CommandEnvironment(gomock.Any(), pr1).
		Return(nil, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, forge.MergeChangeOptions{}).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), &sync.TrunkOptions{ClosedChanges: sync.ClosedChangesIgnore}).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		sync:      mockSync,
	})

	err := h.executePlan(t.Context(), []*mergeItem{{
		branch:   "feat1",
		base:     "main",
		changeID: pr1,
		mergeURL: testRepositoryID{}.ChangeURL(pr1),
	}}, mergeExecutionOptions{
		ReadyCommand: "exit 0",
	})
	require.NoError(t, err)
}

func TestExecutePlan_mergeCommandRequestsThenAwaitsMerge(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForgeForge := forgetest.NewMockForge(ctrl)
	mockForgeForge.EXPECT().
		ID().
		Return("shamhub").
		AnyTimes()
	mockForge.EXPECT().
		Forge().
		Return(mockForgeForge).
		AnyTimes()
	pr1 := fakeChangeID("pr-1")
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head1")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		CommandEnvironment(gomock.Any(), pr1).
		Return(map[string]string{
			"GIT_SPICE_SHAMHUB_CHANGE_NUMBER": "1",
			"GIT_SPICE_BRANCH":                "wrong",
		}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), &sync.TrunkOptions{ClosedChanges: sync.ClosedChangesIgnore}).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		sync:      mockSync,
		logBuffer: &logBuffer,
	})

	err := h.executePlan(t.Context(), []*mergeItem{
		{
			branch:   "feat1",
			base:     "main",
			changeID: pr1,
			headHash: git.Hash("head1"),
			mergeURL: testRepositoryID{}.ChangeURL(pr1),
		},
	}, mergeExecutionOptions{
		Command: strings.Join([]string{
			"test \"$GIT_SPICE_FORGE_ID\" = shamhub",
			"test \"$GIT_SPICE_BRANCH\" = feat1",
			"test \"$GIT_SPICE_BASE_BRANCH\" = main",
			"test \"$GIT_SPICE_TRUNK_BRANCH\" = main",
			"test \"$GIT_SPICE_CHANGE_URL\" = http://example.com/1",
			"test \"$GIT_SPICE_HEAD_SHA\" = head1",
			"test \"$GIT_SPICE_SHAMHUB_CHANGE_NUMBER\" = 1",
			"test -z \"$GIT_SPICE_CHANGE_ID\"",
			"echo command stdout",
			"echo command stderr >&2",
		}, "\n"),
	})
	require.NoError(t, err)

	assert.Contains(t, logBuffer.String(), "INF merge: command stdout")
	assert.Contains(t, logBuffer.String(), "INF merge: command stderr")
}

func TestExecutePlan_mergeCommandFailureFailsItem(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForgeForge := forgetest.NewMockForge(ctrl)
	mockForgeForge.EXPECT().
		ID().
		Return("shamhub").
		AnyTimes()
	mockForge.EXPECT().
		Forge().
		Return(mockForgeForge).
		AnyTimes()
	pr1 := fakeChangeID("pr-1")
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		CommandEnvironment(gomock.Any(), pr1).
		Return(nil, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
	})

	err := h.executePlan(t.Context(), []*mergeItem{
		{
			branch:   "feat1",
			base:     "main",
			changeID: pr1,
			mergeURL: testRepositoryID{}.ChangeURL(pr1),
		},
	}, mergeExecutionOptions{
		Command: "exit 42",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "command exited with status 42")
}

func TestExecutePlan_firstItemAlreadyOnTrunk(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")

	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), &sync.TrunkOptions{ClosedChanges: sync.ClosedChangesIgnore}).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		sync:      mockSync,
		logBuffer: &logBuffer,
	})

	err := h.executePlan(t.Context(), []*mergeItem{{
		branch:   "feat1",
		base:     "main",
		changeID: pr1,
		mergeURL: testRepositoryID{}.ChangeURL(pr1),
	}}, mergeExecutionOptions{})
	require.NoError(t, err)

	assert.NotContains(t,
		logBuffer.String(), "retargeting")
}

func TestLogMergeProgress_deduplicatesRepeatedState(t *testing.T) {
	var logBuffer bytes.Buffer
	progress := newLogMergeProgress(silog.New(&logBuffer, nil))
	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}

	progress.Event(mergeProgressEvent{
		Kind: mergeProgressRetargeting,
		Item: item,
		Base: "main",
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressRetargeting,
		Item: item,
		Base: "main",
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressWaitingForMergeability,
		Item: item,
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressWaitingForMergeability,
		Item: item,
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressMerging,
		Item: item,
		URL:  "http://example.com/1",
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressMerging,
		Item: item,
		URL:  "http://example.com/1",
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressFailed,
		Item: item,
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressSkipped,
		Item: item,
	})

	output := logBuffer.String()
	assert.Equal(t, 1, strings.Count(output,
		"feat1: retargeting pr-1 onto main"))
	assert.Equal(t, 1, strings.Count(output,
		"feat1: waiting for merge readiness"))
	assert.Equal(t, 1, strings.Count(output,
		"feat1: merging pr-1: http://example.com/1"))
	assert.NotContains(t, output,
		"feat1: failed")
	assert.Equal(t, 1, strings.Count(output,
		"feat1: skipped"))
}

func TestLogMergeProgress_waitingForMergeIsDebug(t *testing.T) {
	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}

	var infoBuffer bytes.Buffer
	infoProgress := newLogMergeProgress(silog.New(&infoBuffer, nil))
	infoProgress.Event(mergeProgressEvent{
		Kind: mergeProgressWaitingForMerge,
		Item: item,
	})
	assert.NotContains(t, infoBuffer.String(),
		"feat1: waiting for merge")

	var debugBuffer bytes.Buffer
	debugProgress := newLogMergeProgress(
		silog.New(&debugBuffer, &silog.Options{
			Level: silog.LevelDebug,
		}),
	)
	debugProgress.Event(mergeProgressEvent{
		Kind: mergeProgressWaitingForMerge,
		Item: item,
	})
	assert.Contains(t, debugBuffer.String(),
		"feat1: waiting for merge")
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

	items := []*mergeItem{
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

	err := h.validateSynced(t.Context(), []*mergeItem{
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

	err := h.validateSynced(t.Context(), []*mergeItem{
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

	err := h.validateSynced(t.Context(), []*mergeItem{
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

	err := h.validateSynced(t.Context(), []*mergeItem{
		{
			branch:         "feat1",
			upstreamBranch: "feat1",
		},
	})
	require.NoError(t, err)
}

// testHandlerOpts overrides the collaborators that matter to a test.
// newTestHandler supplies inert collaborators and an in-memory store
// for fields left unset.
type testHandlerOpts struct {
	forgeRepo *forgetest.MockRepository
	store     Store
	service   *MockService
	restack   *MockRestackHandler
	submit    *MockSubmitHandler
	sync      SyncHandler
	gitRepo   *MockGitRepository
	logBuffer *bytes.Buffer
}

type testChangeMetadata fakeChangeID

var _ forge.ChangeMetadata = testChangeMetadata("")

func (c testChangeMetadata) ForgeID() string {
	return "fake"
}

func (c testChangeMetadata) ChangeID() forge.ChangeID {
	return fakeChangeID(c)
}

func (c testChangeMetadata) NavigationCommentID() forge.ChangeCommentID {
	return nil
}

func (c testChangeMetadata) SetNavigationCommentID(forge.ChangeCommentID) {}

type testRepositoryID struct{}

var _ forge.RepositoryID = testRepositoryID{}

func (testRepositoryID) String() string {
	return "example/repo"
}

func (testRepositoryID) ChangeURL(forge.ChangeID) string {
	return "http://example.com/1"
}

// newTestHandler builds a Handler whose unset collaborators stay quiet
// unless the test path calls into them.
// Tests should still pass explicit mocks for collaborators
// whose interactions are part of the behavior under test.
func newTestHandler(
	t *testing.T,
	ctrl *gomock.Controller,
	opts testHandlerOpts,
) *Handler {
	t.Helper()

	log := silog.Nop()
	if opts.logBuffer != nil {
		log = silog.New(opts.logBuffer, nil)
	}

	forgeRepo := forge.Repository(forgetest.NewMockRepository(ctrl))
	if opts.forgeRepo != nil {
		forgeRepo = opts.forgeRepo
	}

	store := Store(statetest.NewMemoryStore(t, "main", "origin", log))
	if opts.store != nil {
		store = opts.store
	}

	service := Service(NewMockService(ctrl))
	if opts.service != nil {
		service = opts.service
	}

	restackHandler := RestackHandler(NewMockRestackHandler(ctrl))
	if opts.restack != nil {
		restackHandler = opts.restack
	}

	submitHandler := SubmitHandler(NewMockSubmitHandler(ctrl))
	if opts.submit != nil {
		submitHandler = opts.submit
	}

	syncHandler := opts.sync
	if syncHandler == nil {
		syncHandler = syncHandlerFunc(
			func(context.Context, *sync.TrunkOptions) error {
				return nil
			},
		)
	}

	gitRepo := GitRepository(NewMockGitRepository(ctrl))
	if opts.gitRepo != nil {
		gitRepo = opts.gitRepo
	}

	return &Handler{
		Log:                log,
		View:               ui.NewFileView(io.Discard),
		Remote:             "origin",
		RemoteRepository:   forgeRepo,
		RemoteRepositoryID: testRepositoryID{},
		Store:              store,
		Service:            service,
		Restack:            restackHandler,
		Submit:             submitHandler,
		Sync:               syncHandler,
		Repository:         gitRepo,
		ScriptRunner:       &scriptrun.Runner{Log: log},
	}
}

type syncHandlerFunc func(context.Context, *sync.TrunkOptions) error

// SyncTrunk adapts a function into SyncHandler
// so tests can provide only the sync behavior they exercise.
func (f syncHandlerFunc) SyncTrunk(
	ctx context.Context,
	opts *sync.TrunkOptions,
) error {
	return f(ctx, opts)
}

func mergePlanBranches(plan mergePlan) []string {
	branches := make([]string, 0, len(plan.items))
	for _, item := range plan.items {
		branches = append(branches, item.branch)
	}
	return branches
}

func assertSubmitUpdate(
	t *testing.T,
	branch string,
) func(context.Context, *submit.Request) error {
	t.Helper()

	return func(_ context.Context, req *submit.Request) error {
		assert.Equal(t, branch, req.Branch)
		require.NotNil(t, req.Options)
		assert.True(t, req.Options.Publish)
		require.NotNil(t, req.Options.UpdateOnly)
		assert.True(t, *req.Options.UpdateOnly)
		return nil
	}
}

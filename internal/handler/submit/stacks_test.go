package submit

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/spicetest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.uber.org/mock/gomock"
)

func TestNativeStackChanges(t *testing.T) {
	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
		Trunk: "main",
		Branches: []spice.LoadBranchItem{
			{Name: "other", Base: "main", Change: submitFakeChange("pr-9")},
			{Name: "top", Base: "middle", Change: submitFakeChange("pr-3")},
			{Name: "bottom", Base: "main", Change: submitFakeChange("pr-1")},
			{Name: "divergent", Base: "middle", Change: submitFakeChange("pr-4")},
			{Name: "middle", Base: "bottom", Change: submitFakeChange("pr-2")},
		},
	})

	got := nativeStackChanges(graph, "test", []string{"top"})
	assert.ElementsMatch(t, []forge.StackChange{
		{Change: submitFakeChangeID("pr-1"), BaseBranch: "main"},
		{Change: submitFakeChangeID("pr-2"), BaseChange: submitFakeChangeID("pr-1"), BaseBranch: "bottom"},
		{Change: submitFakeChangeID("pr-3"), BaseChange: submitFakeChangeID("pr-2"), BaseBranch: "middle"},
		{Change: submitFakeChangeID("pr-4"), BaseChange: submitFakeChangeID("pr-2"), BaseBranch: "middle"},
	}, got)
}

func TestHandler_updateStackRepresentations_unsupported(t *testing.T) {
	ctrl := gomock.NewController(t)
	remoteForge := forgetest.NewMockForge(ctrl)
	remoteRepo := forgetest.NewMockRepository(ctrl)

	service := NewMockService(ctrl)
	handler := new(Handler)
	handler.Log = silog.Nop()
	handler.Service = service
	handler.FindRemote = func(context.Context) (state.Remote, error) {
		return state.Remote{Upstream: "origin"}, nil
	}
	handler.ResolveRepository = func(
		context.Context,
		string,
	) (forge.Forge, forge.RepositoryID, error) {
		return remoteForge, stubRepositoryID("acme/repo"), nil
	}
	handler.OpenRepository = func(
		context.Context,
		forge.Forge,
		forge.RepositoryID,
	) (forge.Repository, error) {
		return remoteRepo, nil
	}

	require.NoError(t, handler.updateStackRepresentations(
		t.Context(),
		&Options{NavComment: NavCommentNever},
		&submitStackUpdates{requestedBranches: []string{"feature"}},
		[]string{"feature"},
		[]string{"feature"},
	))
}

func TestHandler_updateStackRepresentations_nativeStackErrors(t *testing.T) {
	tests := []struct {
		name    string
		planErr error
		runErr  error
		wantLog bool
	}{
		{name: "Unsupported", planErr: forge.ErrUnsupported},
		{name: "Failure", runErr: errors.New("boom"), wantLog: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			remoteForge := forgetest.NewMockForge(ctrl)
			remoteForge.EXPECT().ID().Return("test").AnyTimes()

			remoteRepo := forgetest.NewMockRepository(ctrl)
			remoteRepo.EXPECT().Forge().Return(remoteForge).AnyTimes()
			stackRepo := &submitStackRepository{
				Repository: remoteRepo,
				planErr:    tt.planErr,
				runErr:     tt.runErr,
			}

			graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
				Trunk: "main",
				Branches: []spice.LoadBranchItem{
					{Name: "feature", Base: "main", Change: submitFakeChange("pr-1")},
				},
			})
			service := NewMockService(ctrl)
			service.EXPECT().BranchGraph(gomock.Any(), nil).Return(graph, nil)

			var logs bytes.Buffer
			handler := new(Handler)
			handler.Log = silog.New(&logs, nil)
			handler.Service = service
			handler.FindRemote = func(context.Context) (state.Remote, error) {
				return state.Remote{Upstream: "origin"}, nil
			}
			handler.ResolveRepository = func(
				context.Context,
				string,
			) (forge.Forge, forge.RepositoryID, error) {
				return remoteForge, stubRepositoryID("acme/repo"), nil
			}
			handler.OpenRepository = func(
				context.Context,
				forge.Forge,
				forge.RepositoryID,
			) (forge.Repository, error) {
				return stackRepo, nil
			}

			require.NoError(t, handler.updateStackRepresentations(
				t.Context(),
				&Options{NavComment: NavCommentNever},
				&submitStackUpdates{requestedBranches: []string{"feature"}},
				[]string{"feature"},
				[]string{"feature"},
			))
			require.Len(t, stackRepo.plans, 1)

			if tt.wantLog {
				assert.Contains(t, logs.String(), "Could not update stacks")
				assert.Contains(t, logs.String(), "boom")
			} else {
				assert.Empty(t, logs.String())
			}
		})
	}
}

func TestHandler_updateStackRepresentations_fallsBackAfterUnsupportedPlan(t *testing.T) {
	ctrl := gomock.NewController(t)
	remoteForge := forgetest.NewMockForge(ctrl)
	remoteForge.EXPECT().ID().Return("test").AnyTimes()
	remoteRepo := forgetest.NewMockRepository(ctrl)
	remoteRepo.EXPECT().Forge().Return(remoteForge).AnyTimes()
	change := submitFakeChangeID("pr-1")
	remoteRepo.EXPECT().EditChange(gomock.Any(), change, forge.EditChangeOptions{
		Base: "main",
	}).Return(nil)
	stackRepo := &submitStackRepository{
		Repository: remoteRepo,
		planErr:    forge.ErrUnsupported,
	}

	graph := spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
		Trunk: "main",
		Branches: []spice.LoadBranchItem{
			{Name: "feature", Base: "main", Change: submitFakeChange("pr-1")},
		},
	})
	service := NewMockService(ctrl)
	service.EXPECT().BranchGraph(gomock.Any(), nil).Return(graph, nil)
	handler := newStackFinalizationTestHandler(remoteForge, stackRepo, service)

	err := handler.updateStackRepresentations(
		t.Context(),
		&Options{NavComment: NavCommentNever},
		&submitStackUpdates{
			requestedBranches: []string{"feature"},
			deferredBases: []deferredBaseUpdate{
				{branch: "feature", repository: remoteRepo, change: change, base: "main"},
			},
		},
		[]string{"feature"},
		[]string{"feature"},
	)
	require.NoError(t, err)
}

type submitStackRepository struct {
	forge.Repository

	plans   [][]forge.StackChange
	planErr error
	runErr  error
}

func (r *submitStackRepository) PlanStackUpdate(
	_ context.Context,
	changes []forge.StackChange,
) (forge.StackUpdatePlan, error) {
	r.plans = append(r.plans, changes)
	if r.planErr != nil {
		return nil, r.planErr
	}
	return submitStackUpdatePlan{err: r.runErr}, nil
}

type submitStackUpdatePlan struct{ err error }

func (p submitStackUpdatePlan) Execute(context.Context) error { return p.err }

func newStackFinalizationTestHandler(
	remoteForge forge.Forge,
	remoteRepo forge.Repository,
	service Service,
) *Handler {
	handler := new(Handler)
	handler.Log = silog.Nop()
	handler.Service = service
	handler.FindRemote = func(context.Context) (state.Remote, error) {
		return state.Remote{Upstream: "origin"}, nil
	}
	handler.ResolveRepository = func(
		context.Context,
		string,
	) (forge.Forge, forge.RepositoryID, error) {
		return remoteForge, stubRepositoryID("acme/repo"), nil
	}
	handler.OpenRepository = func(
		context.Context,
		forge.Forge,
		forge.RepositoryID,
	) (forge.Repository, error) {
		return remoteRepo, nil
	}
	return handler
}

func (*submitStackRepository) PlanMergeRanges(
	context.Context,
	[]forge.StackChange,
) ([]forge.MergeRangePlan, error) {
	return nil, forge.ErrUnsupported
}

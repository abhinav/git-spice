package submit

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/browser"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
	gomock "go.uber.org/mock/gomock"
)

func TestBranchSubmit_listChangeTemplates(t *testing.T) {
	t.Run("NoTimeout", func(t *testing.T) {
		log := silogtest.New(t)
		ctx := t.Context()
		tmpl := &forge.ChangeTemplate{}
		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			ListChangeTemplates(
				gomock.Cond(func(ctx context.Context) bool {
					_, ok := ctx.Deadline()
					return assert.False(t, ok, "context should not have a deadline")
				}), gomock.Any(), gomock.Any()).
			Return([]*forge.ChangeTemplate{tmpl}, nil)

		got := listChangeTemplates(ctx, mockService, log, "origin", nil, &Options{})
		if assert.Len(t, got, 1) {
			assert.Same(t, tmpl, got[0])
		}
	})

	t.Run("Timeout", func(t *testing.T) {
		log := silogtest.New(t)
		ctx := t.Context()

		ctrl := gomock.NewController(t)
		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			ListChangeTemplates(
				gomock.Cond(func(ctx context.Context) bool {
					_, ok := ctx.Deadline()
					return assert.True(t, ok, "context should have a deadline")
				}), gomock.Any(), gomock.Any()).
			Return(nil, errors.New("great sadness"))

		got := listChangeTemplates(ctx, mockService, log, "origin", nil, &Options{
			ListTemplatesTimeout: time.Second,
		})
		assert.Empty(t, got)
	})
}

func TestHandler_pushRepositoryID_rejectsDifferentForge(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	upstreamForge := forgetest.NewMockForge(mockCtrl)
	upstreamForge.EXPECT().
		ID().
		Return("github").
		AnyTimes()

	pushForge := forgetest.NewMockForge(mockCtrl)
	pushForge.EXPECT().
		ID().
		Return("gitlab").
		AnyTimes()

	handler := &Handler{
		Log:        silog.Nop(),
		View:       ui.NewFileView(&bytes.Buffer{}),
		Repository: nil,
		Worktree:   nil,
		Store:      NewMockStore(mockCtrl),
		Service:    NewMockService(mockCtrl),
		Browser:    &browser.Noop{},
		FindRemote: func(context.Context) (state.Remote, error) {
			return state.Remote{
				Upstream: "upstream",
				Push:     "origin",
			}, nil
		},
		OpenRepository: func(context.Context, forge.Forge, forge.RepositoryID) (forge.Repository, error) {
			return nil, assert.AnError
		},
		ResolveRepository: func(
			_ context.Context,
			remote string,
		) (forge.Forge, forge.RepositoryID, error) {
			switch remote {
			case "upstream":
				return upstreamForge, stubRepositoryID("alice/repo"), nil
			case "origin":
				return pushForge, stubRepositoryID("bob/repo"), nil
			default:
				return nil, nil, assert.AnError
			}
		},
	}

	_, err := handler.pushRepositoryID(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different forge")
}

func TestReviewersAddWhen_UnmarshalText(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    ReviewersAddWhen
		wantErr string
	}{
		{
			name:  "Always",
			input: "always",
			want:  ReviewersAddWhenAlways,
		},
		{
			name:  "Ready",
			input: "ready",
			want:  ReviewersAddWhenReady,
		},
		{
			name:    "Invalid",
			input:   "never",
			wantErr: `invalid value "never": expected always or ready`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ReviewersAddWhen
			err := got.UnmarshalText([]byte(tt.input))

			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReviewersAddWhen_String(t *testing.T) {
	tests := []struct {
		name  string
		value ReviewersAddWhen
		want  string
	}{
		{name: "Always", value: ReviewersAddWhenAlways, want: "always"},
		{name: "Ready", value: ReviewersAddWhenReady, want: "ready"},
		{name: "Unknown", value: ReviewersAddWhen(99), want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.value.String())
		})
	}
}

func TestEffectiveReviewers(t *testing.T) {
	tests := []struct {
		name                string
		addWhen             ReviewersAddWhen
		isDraft             bool
		flagReviewers       []string
		configuredReviewers []string
		want                []string
	}{
		{
			name:                "AlwaysDraft",
			addWhen:             ReviewersAddWhenAlways,
			isDraft:             true,
			flagReviewers:       []string{"alice"},
			configuredReviewers: []string{"bob"},
			want:                []string{"alice", "bob"},
		},
		{
			name:                "AlwaysReady",
			addWhen:             ReviewersAddWhenAlways,
			isDraft:             false,
			flagReviewers:       []string{"alice"},
			configuredReviewers: []string{"bob"},
			want:                []string{"alice", "bob"},
		},
		{
			name:                "ReadyDraft",
			addWhen:             ReviewersAddWhenReady,
			isDraft:             true,
			flagReviewers:       []string{"alice"},
			configuredReviewers: []string{"bob"},
			want:                []string{"alice"},
		},
		{
			name:                "ReadyNotDraft",
			addWhen:             ReviewersAddWhenReady,
			isDraft:             false,
			flagReviewers:       []string{"alice"},
			configuredReviewers: []string{"bob"},
			want:                []string{"alice", "bob"},
		},
		{
			name:                "ReadyDraftNoFlags",
			addWhen:             ReviewersAddWhenReady,
			isDraft:             true,
			flagReviewers:       nil,
			configuredReviewers: []string{"bob"},
			want:                nil,
		},
		{
			name:                "Deduplication",
			addWhen:             ReviewersAddWhenAlways,
			isDraft:             false,
			flagReviewers:       []string{"alice", "bob"},
			configuredReviewers: []string{"bob", "charlie"},
			want:                []string{"alice", "bob", "charlie"},
		},
		{
			name:                "EmptyBoth",
			addWhen:             ReviewersAddWhenAlways,
			isDraft:             false,
			flagReviewers:       nil,
			configuredReviewers: nil,
			want:                nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &Options{
				Reviewers:           tt.flagReviewers,
				ConfiguredReviewers: tt.configuredReviewers,
				ReviewersAddWhen:    tt.addWhen,
			}
			got := effectiveReviewers(opts, tt.isDraft)
			assert.Equal(t, tt.want, got)
		})
	}
}

type stubRepositoryID string

func (id stubRepositoryID) String() string {
	return string(id)
}

func (id stubRepositoryID) ChangeURL(forge.ChangeID) string {
	return string(id)
}

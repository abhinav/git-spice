package bitbucket

import (
	"bytes"
	"errors"
	"fmt"
	"iter"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	gw "go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.uber.org/mock/gomock"
)

func TestRepository_Forge(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	f := new(Forge)
	repo := newRepository(f, silog.Nop(), NewMockGateway(mockCtrl))
	assert.Same(t, f, repo.Forge())
}

func TestRepository_ChangeURL(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		ChangeURL(int64(42)).
		Return("https://example.com/pr/42")

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	assert.Equal(t,
		"https://example.com/pr/42",
		repo.ChangeURL(&PR{Number: 42}))
}

func TestRepository_NewChangeMetadata(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	repo := newRepository(
		new(Forge), silog.Nop(), NewMockGateway(mockCtrl),
	)
	md, err := repo.NewChangeMetadata(t.Context(), &PR{Number: 42})
	require.NoError(t, err)

	assert.Equal(t, &PRMetadata{PR: &PR{Number: 42}}, md)
}

func TestRepository_SubmitChange(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		CreateChange(gomock.Any(), gw.CreateChangeRequest{
			Subject:        "Test PR",
			Body:           "Description",
			Base:           "main",
			Head:           "feature",
			PushRepository: &RepositoryID{url: "https://example.com", workspace: "fork", name: "repo"},
			Draft:          true,
			Reviewers:      []string{"reviewer1"},
		}).
		Return(&gw.PullRequest{
			Number: 123,
			URL:    "https://example.com/pr/123",
		}, nil)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	result, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject:        "Test PR",
		Body:           "Description",
		Base:           "main",
		Head:           "feature",
		PushRepository: &RepositoryID{url: "https://example.com", workspace: "fork", name: "repo"},
		Draft:          true,
		Reviewers:      []string{"reviewer1"},
	})
	require.NoError(t, err)

	assert.Equal(t, &PR{Number: 123}, result.ID)
	assert.Equal(t, "https://example.com/pr/123", result.URL)
}

func TestRepository_SubmitChange_unsupportedWarnings(t *testing.T) {
	tests := []struct {
		name    string
		product string

		wantLabelWarning    string
		wantAssigneeWarning string
	}{
		{
			name:    "Cloud",
			product: "Bitbucket",
			wantLabelWarning: "Bitbucket does not support PR labels; " +
				"ignoring --label flags",
			wantAssigneeWarning: "Bitbucket does not support PR assignees; " +
				"ignoring --assign flags",
		},
		{
			name:    "DataCenter",
			product: "Bitbucket Data Center",
			wantLabelWarning: "Bitbucket Data Center does not support " +
				"PR labels; ignoring --label flags",
			wantAssigneeWarning: "Bitbucket Data Center does not support " +
				"PR assignees; ignoring --assign flags",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)

			mockGateway := NewMockGateway(mockCtrl)
			mockGateway.EXPECT().
				Product().
				Return(tt.product).
				Times(2)
			mockGateway.EXPECT().
				CreateChange(gomock.Any(), gomock.Any()).
				Return(&gw.PullRequest{Number: 1}, nil)

			var logBuffer bytes.Buffer
			repo := newRepository(
				new(Forge), silog.New(&logBuffer, nil), mockGateway,
			)
			_, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
				Subject:   "Test PR",
				Base:      "main",
				Head:      "feature",
				Labels:    []string{"bug"},
				Assignees: []string{"someone"},
			})
			require.NoError(t, err)

			assert.Contains(t, logBuffer.String(), tt.wantLabelWarning)
			assert.Contains(t, logBuffer.String(), tt.wantAssigneeWarning)
		})
	}
}

func TestRepository_SubmitChange_error(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	wantErr := errors.New("create pull request: boom")
	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		CreateChange(gomock.Any(), gomock.Any()).
		Return(nil, wantErr)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	_, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Test PR",
		Base:    "main",
		Head:    "feature",
	})
	assert.ErrorIs(t, err, wantErr)
}

func TestRepository_EditChange_baseAndReviewers(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		UpdateChange(gomock.Any(), int64(7), gw.ChangeUpdate{
			Base:         "develop",
			AddReviewers: []string{"spock"},
		}).
		Return(nil)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	require.NoError(t, repo.EditChange(t.Context(), &PR{Number: 7},
		forge.EditChangeOptions{
			Base:         "develop",
			AddReviewers: []string{"spock"},
		}))
}

func TestRepository_EditChange_noChangesIsNoop(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	repo := newRepository(
		new(Forge), silog.Nop(), NewMockGateway(mockCtrl),
	)
	require.NoError(t,
		repo.EditChange(t.Context(), &PR{Number: 7},
			forge.EditChangeOptions{}))
}

func TestRepository_EditChange_draft(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	draft := true
	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		SetChangeDraft(gomock.Any(), int64(7), true).
		Return(nil)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	require.NoError(t, repo.EditChange(t.Context(), &PR{Number: 7},
		forge.EditChangeOptions{Draft: &draft}))
}

func TestRepository_EditChange_draftUnsupported(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	draft := false
	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		SetChangeDraft(gomock.Any(), int64(7), false).
		Return(fmt.Errorf("set draft: %w", gw.ErrUnsupported))
	mockGateway.EXPECT().
		Product().
		Return("Bitbucket Data Center")

	var logBuffer bytes.Buffer
	repo := newRepository(
		new(Forge), silog.New(&logBuffer, nil), mockGateway,
	)
	require.NoError(t, repo.EditChange(t.Context(), &PR{Number: 7},
		forge.EditChangeOptions{Draft: &draft}))

	assert.Contains(t, logBuffer.String(),
		"Bitbucket Data Center does not support toggling PR draft status "+
			"after creation; ignoring --draft/--ready")
}

func TestRepository_EditChange_draftError(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	draft := true
	wantErr := errors.New("update pull request: boom")
	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		SetChangeDraft(gomock.Any(), int64(7), true).
		Return(wantErr)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	err := repo.EditChange(t.Context(), &PR{Number: 7},
		forge.EditChangeOptions{Draft: &draft})
	assert.ErrorIs(t, err, wantErr)
}

func TestRepository_EditChange_unsupportedWarnings(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		Product().
		Return("Bitbucket").
		Times(2)

	var logBuffer bytes.Buffer
	repo := newRepository(
		new(Forge), silog.New(&logBuffer, nil), mockGateway,
	)
	require.NoError(t, repo.EditChange(t.Context(), &PR{Number: 7},
		forge.EditChangeOptions{
			AddLabels:    []string{"bug"},
			AddAssignees: []string{"someone"},
		}))

	assert.Contains(t, logBuffer.String(),
		"Bitbucket does not support PR labels; ignoring --label flags")
	assert.Contains(t, logBuffer.String(),
		"Bitbucket does not support PR assignees; ignoring --assign flags")
}

func TestRepository_MergeChange(t *testing.T) {
	methods := []forge.MergeMethod{
		forge.MergeMethodDefault,
		forge.MergeMethodMerge,
		forge.MergeMethodSquash,
		forge.MergeMethodRebase,
		forge.MergeMethod(99),
	}

	for _, method := range methods {
		t.Run(fmt.Sprintf("Method%d", method), func(t *testing.T) {
			mockCtrl := gomock.NewController(t)

			mockGateway := NewMockGateway(mockCtrl)
			mockGateway.EXPECT().
				MergeChange(gomock.Any(), int64(5), method).
				Return(nil)

			repo := newRepository(new(Forge), silog.Nop(), mockGateway)
			require.NoError(t, repo.MergeChange(t.Context(), &PR{Number: 5},
				forge.MergeChangeOptions{Method: method}))
		})
	}
}

func TestRepository_MergeChange_blockedPassthrough(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		MergeChange(gomock.Any(), int64(5), forge.MergeMethodDefault).
		Return(fmt.Errorf("%w: %s", gw.ErrMergeBlocked, "requires 2 approvals"))

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	err := repo.MergeChange(t.Context(), &PR{Number: 5},
		forge.MergeChangeOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, gw.ErrMergeBlocked)

	assert.Equal(t,
		"pull request cannot be merged: requires 2 approvals",
		err.Error())
}

func TestRepository_ChangeStatuses(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		GetChange(gomock.Any(), int64(1)).
		Return(&gw.PullRequest{
			Number:   1,
			State:    forge.ChangeOpen,
			HeadHash: git.Hash("abc123"),
		}, nil)
	mockGateway.EXPECT().
		GetChange(gomock.Any(), int64(2)).
		Return(&gw.PullRequest{
			Number:   2,
			State:    forge.ChangeMerged,
			HeadHash: git.Hash("def456"),
		}, nil)
	mockGateway.EXPECT().
		GetChange(gomock.Any(), int64(3)).
		Return(&gw.PullRequest{Number: 3, State: forge.ChangeClosed}, nil)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	statuses, err := repo.ChangeStatuses(t.Context(), []forge.ChangeID{
		&PR{Number: 1}, &PR{Number: 2}, &PR{Number: 3},
	})
	require.NoError(t, err)

	assert.Equal(t, []forge.ChangeStatus{
		{State: forge.ChangeOpen, HeadHash: git.Hash("abc123")},
		{State: forge.ChangeMerged, HeadHash: git.Hash("def456")},
		{State: forge.ChangeClosed},
	}, statuses)
}

func TestRepository_ChangeStatuses_error(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		GetChange(gomock.Any(), int64(1)).
		Return(&gw.PullRequest{Number: 1, State: forge.ChangeOpen}, nil)
	mockGateway.EXPECT().
		GetChange(gomock.Any(), int64(2)).
		Return(nil, errors.New("get pull request: boom"))

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	_, err := repo.ChangeStatuses(t.Context(), []forge.ChangeID{
		&PR{Number: 1}, &PR{Number: 2},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "get state for PR #2")
}

func TestRepository_ChangeChecks(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	want := []forge.ChangeCheck{
		{Name: "build", State: forge.ChangeCheckPassed},
		{Name: "test", State: forge.ChangeCheckPending},
	}
	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		GetChange(gomock.Any(), int64(7)).
		Return(&gw.PullRequest{
			Number:   7,
			HeadHash: git.Hash("feedface"),
		}, nil)
	mockGateway.EXPECT().
		ListCommitChecks(gomock.Any(), git.Hash("feedface")).
		Return(want, nil)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	got, err := repo.ChangeChecks(t.Context(), &PR{Number: 7})
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestRepository_ChangeChecks_noHeadCommit(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		GetChange(gomock.Any(), int64(7)).
		Return(&gw.PullRequest{Number: 7}, nil)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	got, err := repo.ChangeChecks(t.Context(), &PR{Number: 7})
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestRepository_ChangeChecks_errors(t *testing.T) {
	t.Run("GetChange", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)

		wantErr := errors.New("get pull request: boom")
		mockGateway := NewMockGateway(mockCtrl)
		mockGateway.EXPECT().
			GetChange(gomock.Any(), int64(7)).
			Return(nil, wantErr)

		repo := newRepository(new(Forge), silog.Nop(), mockGateway)
		_, err := repo.ChangeChecks(t.Context(), &PR{Number: 7})
		assert.ErrorIs(t, err, wantErr)
	})

	t.Run("ListCommitChecks", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)

		wantErr := errors.New("list build statuses: boom")
		mockGateway := NewMockGateway(mockCtrl)
		mockGateway.EXPECT().
			GetChange(gomock.Any(), int64(7)).
			Return(&gw.PullRequest{
				Number:   7,
				HeadHash: git.Hash("feedface"),
			}, nil)
		mockGateway.EXPECT().
			ListCommitChecks(gomock.Any(), git.Hash("feedface")).
			Return(nil, wantErr)

		repo := newRepository(new(Forge), silog.Nop(), mockGateway)
		_, err := repo.ChangeChecks(t.Context(), &PR{Number: 7})
		assert.ErrorIs(t, err, wantErr)
	})
}

func TestRepository_CommentCountsByChange(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		ResolvableComments(gomock.Any(), int64(1)).
		Return(yieldAll(
			&gw.ResolvableComment{ID: 10, Body: "resolved", Resolved: true},
			&gw.ResolvableComment{ID: 11, Body: "open"},
			&gw.ResolvableComment{ID: 12, Body: "draft", Pending: true},
			&gw.ResolvableComment{
				ID:   13,
				Body: "stack\n\n" + _navigationCommentMarker,
			},
		))
	mockGateway.EXPECT().
		ResolvableComments(gomock.Any(), int64(2)).
		Return(yieldAll[*gw.ResolvableComment]())

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	got, err := repo.CommentCountsByChange(t.Context(), []forge.ChangeID{
		&PR{Number: 1}, &PR{Number: 2},
	})
	require.NoError(t, err)

	assert.Equal(t, []*forge.CommentCounts{
		{Total: 2, Resolved: 1, Unresolved: 1},
		{Total: 0, Resolved: 0, Unresolved: 0},
	}, got)
}

func TestRepository_CommentCountsByChange_empty(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	repo := newRepository(
		new(Forge), silog.Nop(), NewMockGateway(mockCtrl),
	)
	got, err := repo.CommentCountsByChange(t.Context(), nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestRepository_CommentCountsByChange_error(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		ResolvableComments(gomock.Any(), int64(1)).
		Return(yieldErr[*gw.ResolvableComment](
			errors.New("list activities: boom"),
		))

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	_, err := repo.CommentCountsByChange(t.Context(), []forge.ChangeID{
		&PR{Number: 1},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "get counts for #1")
}

func TestRepository_ListChangeComments(t *testing.T) {
	tests := []struct {
		name     string
		comments []*gw.ChangeComment
		opts     *forge.ListChangeCommentsOptions

		wantBodies []string
	}{
		{
			name: "NoFilter",
			comments: []*gw.ChangeComment{
				{ID: 1, PRID: 7, Body: "hello"},
				{ID: 2, PRID: 7, Body: "world"},
			},
			wantBodies: []string{"hello", "world"},
		},
		{
			name: "BodyMatchesAll",
			comments: []*gw.ChangeComment{
				{ID: 1, PRID: 7, Body: "hello"},
				{ID: 2, PRID: 7, Body: "world"},
			},
			opts: &forge.ListChangeCommentsOptions{
				BodyMatchesAll: []*regexp.Regexp{
					regexp.MustCompile(`d$`),
				},
			},
			wantBodies: []string{"world"},
		},
		{
			name: "BodyMatchesAllMultiple",
			comments: []*gw.ChangeComment{
				{ID: 1, PRID: 7, Body: "hello world"},
				{ID: 2, PRID: 7, Body: "world"},
				{ID: 3, PRID: 7, Body: "hello"},
			},
			opts: &forge.ListChangeCommentsOptions{
				BodyMatchesAll: []*regexp.Regexp{
					regexp.MustCompile(`hello`),
					regexp.MustCompile(`world`),
				},
			},
			wantBodies: []string{"hello world"},
		},
		{
			name:       "EmptyList",
			wantBodies: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)

			mockGateway := NewMockGateway(mockCtrl)
			mockGateway.EXPECT().
				ListComments(gomock.Any(), int64(7), gw.ListCommentsOptions{}).
				Return(yieldAll(tt.comments...))

			repo := newRepository(new(Forge), silog.Nop(), mockGateway)

			var bodies []string
			for comment, err := range repo.ListChangeComments(
				t.Context(), &PR{Number: 7}, tt.opts,
			) {
				require.NoError(t, err)
				bodies = append(bodies, comment.Body)
			}

			assert.Equal(t, tt.wantBodies, bodies)
		})
	}
}

func TestRepository_ListChangeComments_commentID(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		ListComments(gomock.Any(), int64(7), gw.ListCommentsOptions{}).
		Return(yieldAll(
			&gw.ChangeComment{ID: 101, PRID: 7, Version: 4, Body: "hi"},
		))

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)

	var items []*forge.ListChangeCommentItem
	for item, err := range repo.ListChangeComments(
		t.Context(), &PR{Number: 7}, nil,
	) {
		require.NoError(t, err)
		items = append(items, item)
	}

	assert.Equal(t, []*forge.ListChangeCommentItem{
		{
			ID:   &PRComment{ID: 101, PRID: 7, Version: 4},
			Body: "hi",
		},
	}, items)
}

func TestRepository_ListChangeComments_canUpdate(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		ListComments(gomock.Any(), int64(7), gw.ListCommentsOptions{
			CanUpdateOnly: true,
		}).
		Return(yieldAll[*gw.ChangeComment]())

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	for _, err := range repo.ListChangeComments(
		t.Context(), &PR{Number: 7},
		&forge.ListChangeCommentsOptions{CanUpdate: true},
	) {
		require.NoError(t, err)
	}
}

func TestRepository_ListChangeComments_error(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	wantErr := errors.New("list comments: boom")
	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		ListComments(gomock.Any(), int64(7), gw.ListCommentsOptions{}).
		Return(yieldErr[*gw.ChangeComment](wantErr))

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)

	var gotErr error
	for _, err := range repo.ListChangeComments(
		t.Context(), &PR{Number: 7}, nil,
	) {
		gotErr = err
	}
	assert.ErrorIs(t, gotErr, wantErr)
}

func TestRepository_PostChangeComment(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		CreateComment(gomock.Any(), int64(7), "hello world").
		Return(&gw.ChangeComment{ID: 101, PRID: 7, Version: 3}, nil)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	id, err := repo.PostChangeComment(
		t.Context(), &PR{Number: 7}, "hello world",
	)
	require.NoError(t, err)

	assert.Equal(t, &PRComment{ID: 101, PRID: 7, Version: 3}, id)
}

func TestRepository_PostChangeComment_error(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	wantErr := errors.New("create comment: boom")
	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		CreateComment(gomock.Any(), int64(7), "hello").
		Return(nil, wantErr)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	_, err := repo.PostChangeComment(t.Context(), &PR{Number: 7}, "hello")
	assert.ErrorIs(t, err, wantErr)
}

func TestRepository_UpdateChangeComment(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		UpdateComment(gomock.Any(),
			&gw.ChangeComment{ID: 101, PRID: 7, Version: 2}, "updated body").
		Return(nil)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	require.NoError(t, repo.UpdateChangeComment(t.Context(),
		&PRComment{ID: 101, PRID: 7, Version: 2}, "updated body"))
}

func TestRepository_UpdateChangeComment_missingPRID(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	repo := newRepository(
		new(Forge), silog.Nop(), NewMockGateway(mockCtrl),
	)
	err := repo.UpdateChangeComment(t.Context(),
		&PRComment{ID: 42}, "updated body")
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrCommentCannotUpdate)
	assert.ErrorContains(t, err, "comment 42 missing PR ID")
}

func TestRepository_UpdateChangeComment_notFoundPassthrough(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		UpdateComment(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(fmt.Errorf("comment 101 not found: %w", forge.ErrNotFound))

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	err := repo.UpdateChangeComment(t.Context(),
		&PRComment{ID: 101, PRID: 7}, "updated body")
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrNotFound)
}

func TestRepository_DeleteChangeComment(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		DeleteComment(gomock.Any(),
			&gw.ChangeComment{ID: 101, PRID: 7, Version: 4}).
		Return(nil)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	require.NoError(t, repo.DeleteChangeComment(t.Context(),
		&PRComment{ID: 101, PRID: 7, Version: 4}))
}

func TestRepository_DeleteChangeComment_missingPRID(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	repo := newRepository(
		new(Forge), silog.Nop(), NewMockGateway(mockCtrl),
	)
	err := repo.DeleteChangeComment(t.Context(), &PRComment{ID: 42})
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrCommentCannotUpdate)
	assert.ErrorContains(t, err, "comment 42 missing PR ID")
}

func TestRepository_FindChangesByBranch(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		FindChangesByBranch(gomock.Any(), "feature", gw.FindChangesOptions{
			State: forge.ChangeOpen,
			Limit: 10,
		}).
		Return([]*gw.PullRequest{
			{
				Number:    11,
				URL:       "https://example.com/pr/11",
				State:     forge.ChangeOpen,
				Subject:   "Refit the warp core",
				BaseName:  "develop",
				HeadHash:  git.Hash("abc123"),
				Draft:     true,
				Reviewers: []string{"spock", "uhura"},
			},
		}, nil)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	items, err := repo.FindChangesByBranch(t.Context(), "feature",
		forge.FindChangesOptions{State: forge.ChangeOpen})
	require.NoError(t, err)

	assert.Equal(t, []*forge.FindChangeItem{
		{
			ID:        &PR{Number: 11},
			URL:       "https://example.com/pr/11",
			State:     forge.ChangeOpen,
			Subject:   "Refit the warp core",
			BaseName:  "develop",
			HeadHash:  git.Hash("abc123"),
			Draft:     true,
			Reviewers: []string{"spock", "uhura"},
		},
	}, items)
}

func TestRepository_FindChangesByBranch_limit(t *testing.T) {
	tests := []struct {
		name  string
		limit int

		wantLimit int
	}{
		{name: "Default", limit: 0, wantLimit: 10},
		{name: "Negative", limit: -1, wantLimit: 10},
		{name: "Explicit", limit: 3, wantLimit: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)

			mockGateway := NewMockGateway(mockCtrl)
			mockGateway.EXPECT().
				FindChangesByBranch(gomock.Any(), "feature",
					gw.FindChangesOptions{Limit: tt.wantLimit}).
				Return(nil, nil)

			repo := newRepository(new(Forge), silog.Nop(), mockGateway)
			_, err := repo.FindChangesByBranch(t.Context(), "feature",
				forge.FindChangesOptions{Limit: tt.limit})
			require.NoError(t, err)
		})
	}
}

func TestRepository_FindChangesByBranch_pushRepository(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	pushRepo := &RepositoryID{url: "https://example.com", workspace: "fork", name: "repo"}
	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		FindChangesByBranch(gomock.Any(), "feature", gw.FindChangesOptions{
			PushRepository: pushRepo,
			Limit:          10,
		}).
		Return(nil, nil)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	_, err := repo.FindChangesByBranch(t.Context(), "feature",
		forge.FindChangesOptions{PushRepository: pushRepo})
	require.NoError(t, err)
}

func TestRepository_FindChangesByBranch_error(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	wantErr := errors.New("list pull requests: boom")
	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		FindChangesByBranch(gomock.Any(), "feature", gomock.Any()).
		Return(nil, wantErr)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	_, err := repo.FindChangesByBranch(t.Context(), "feature",
		forge.FindChangesOptions{})
	assert.ErrorIs(t, err, wantErr)
}

func TestRepository_FindChangeByID(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		GetChange(gomock.Any(), int64(42)).
		Return(&gw.PullRequest{
			Number:   42,
			URL:      "https://example.com/pr/42",
			State:    forge.ChangeMerged,
			Subject:  "Test PR",
			BaseName: "main",
			HeadHash: git.Hash("deadbeef"),
		}, nil)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	item, err := repo.FindChangeByID(t.Context(), &PR{Number: 42})
	require.NoError(t, err)

	assert.Equal(t, &forge.FindChangeItem{
		ID:       &PR{Number: 42},
		URL:      "https://example.com/pr/42",
		State:    forge.ChangeMerged,
		Subject:  "Test PR",
		BaseName: "main",
		HeadHash: git.Hash("deadbeef"),
		Draft:    false,
	}, item)
}

func TestRepository_FindChangeByID_error(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	wantErr := errors.New("get pull request: boom")
	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		GetChange(gomock.Any(), int64(42)).
		Return(nil, wantErr)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	_, err := repo.FindChangeByID(t.Context(), &PR{Number: 42})
	assert.ErrorIs(t, err, wantErr)
}

func TestRepository_ListChangeTemplates(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	f := new(Forge)
	paths := f.ChangeTemplatePaths()
	require.Len(t, paths, 4)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		ChangeTemplate(gomock.Any(), paths[0]).
		Return("## Summary\n", nil)
	mockGateway.EXPECT().
		ChangeTemplate(gomock.Any(), paths[1]).
		Return("", fmt.Errorf("template not found: %w", forge.ErrNotFound))
	mockGateway.EXPECT().
		ChangeTemplate(gomock.Any(), paths[2]).
		Return("nested template\n", nil)
	mockGateway.EXPECT().
		ChangeTemplate(gomock.Any(), paths[3]).
		Return("", fmt.Errorf("template not found: %w", forge.ErrNotFound))

	repo := newRepository(f, silog.Nop(), mockGateway)
	templates, err := repo.ListChangeTemplates(t.Context())
	require.NoError(t, err)

	assert.Equal(t, []*forge.ChangeTemplate{
		{Filename: "PULL_REQUEST_TEMPLATE.md", Body: "## Summary\n"},
		{Filename: "PULL_REQUEST_TEMPLATE.md", Body: "nested template\n"},
	}, templates)
}

func TestRepository_ListChangeTemplates_none(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		ChangeTemplate(gomock.Any(), gomock.Any()).
		Return("", fmt.Errorf("template not found: %w", forge.ErrNotFound)).
		Times(4)

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	templates, err := repo.ListChangeTemplates(t.Context())
	require.NoError(t, err)
	assert.Empty(t, templates)
}

func TestRepository_ListChangeTemplates_error(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockGateway := NewMockGateway(mockCtrl)
	mockGateway.EXPECT().
		ChangeTemplate(gomock.Any(), "PULL_REQUEST_TEMPLATE.md").
		Return("", errors.New("boom"))

	repo := newRepository(new(Forge), silog.Nop(), mockGateway)
	_, err := repo.ListChangeTemplates(t.Context())
	require.Error(t, err)
	assert.ErrorContains(t, err, `get template "PULL_REQUEST_TEMPLATE.md"`)
}

func yieldAll[T any](values ...T) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for _, v := range values {
			if !yield(v, nil) {
				return
			}
		}
	}
}

func yieldErr[T any](err error) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		yield(zero, err)
	}
}

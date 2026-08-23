package cloud

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

func TestGateway_CreateReviewComment(t *testing.T) {
	tests := []struct {
		name string
		req  bitbucket.CreateReviewCommentRequest
		want CommentCreateRequest
	}{
		{
			name: "RightRange",
			req: bitbucket.CreateReviewCommentRequest{
				Path:  "review.go",
				Range: forge.ReviewThreadRange{StartLine: 3, EndLine: 5},
				Side:  forge.ReviewThreadSideRight,
				Body:  "right range",
			},
			want: CommentCreateRequest{
				Content: Content{Raw: "right range"},
				Inline:  &Inline{Path: "review.go", StartTo: new(3), To: new(5)},
			},
		},
		{
			name: "LeftLine",
			req: bitbucket.CreateReviewCommentRequest{
				Path:  "review.go",
				Range: forge.ReviewThreadLine(4),
				Side:  forge.ReviewThreadSideLeft,
				Body:  "left line",
			},
			want: CommentCreateRequest{
				Content: Content{Raw: "left line"},
				Inline:  &Inline{Path: "review.go", From: new(4)},
			},
		},
		{
			name: "FileLevel",
			req: bitbucket.CreateReviewCommentRequest{
				Path: "review.go",
				Side: forge.ReviewThreadSide(99),
				Body: "whole file",
			},
			want: CommentCreateRequest{
				Content: Content{Raw: "whole file"},
				Inline:  &Inline{Path: "review.go"},
			},
		},
		{
			name: "Reply",
			req: bitbucket.CreateReviewCommentRequest{
				ParentID: 10,
				Body:     "reply",
			},
			want: CommentCreateRequest{
				Content: Content{Raw: "reply"},
				Parent:  &CommentRef{ID: 10},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t,
					"/repositories/workspace/repo/pullrequests/1/comments",
					r.URL.Path)

				var got CommentCreateRequest
				require.NoError(t, json.UnmarshalRead(r.Body, &got))
				assert.Equal(t, tt.want, got)
				require.NoError(t, json.MarshalWrite(w, Comment{
					ID:      42,
					Content: got.Content,
					User:    User{Nickname: "spock"},
				}))
			}))
			defer srv.Close()

			comment, err := newTestGateway(t, srv.URL).
				CreateReviewComment(t.Context(), 1, tt.req)
			require.NoError(t, err)
			assert.Equal(t, &bitbucket.ReviewComment{
				ID:     42,
				Body:   tt.req.Body,
				Author: "spock",
			}, comment)
		})
	}
}

func TestGateway_ListReviewThreads_fileLevel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, json.MarshalWrite(w, CommentList{Values: []Comment{{
			ID:      10,
			Content: Content{Raw: "whole file"},
			Inline:  &Inline{Path: "review.go"},
		}}}))
	}))
	defer srv.Close()

	var got []*bitbucket.ReviewThread
	for thread, err := range newTestGateway(t, srv.URL).
		ListReviewThreads(t.Context(), 7) {
		require.NoError(t, err)
		got = append(got, thread)
	}

	require.Len(t, got, 1)
	assert.Equal(t, "review.go", got[0].Path)
	assert.True(t, got[0].Range.IsZero())
}

func TestGateway_ListReviewThreads_rejectsMalformedInline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, json.MarshalWrite(w, CommentList{Values: []Comment{{
			ID:      10,
			Content: Content{Raw: "missing endpoint"},
			Inline:  &Inline{Path: "review.go", StartTo: new(3)},
		}}}))
	}))
	defer srv.Close()

	var gotErr error
	for _, err := range newTestGateway(t, srv.URL).
		ListReviewThreads(t.Context(), 7) {
		gotErr = err
	}
	require.ErrorContains(t, gotErr, "malformed inline anchor")
}

func TestGateway_ListReviewThreads(t *testing.T) {
	created := time.Date(2026, time.June, 14, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		require.NoError(t, json.MarshalWrite(w, CommentList{Values: []Comment{
			{ID: 1, Content: Content{Raw: "global"}},
			{
				ID:         10,
				Content:    Content{Raw: "root"},
				Inline:     &Inline{Path: "review.go", StartFrom: new(3), From: new(5)},
				Resolution: &Resolution{Type: "resolved"},
				User:       User{Nickname: "spock"},
				CreatedOn:  created,
			},
			{
				ID:        11,
				Content:   Content{Raw: "reply"},
				Inline:    &Inline{Path: "review.go", StartFrom: new(3), From: new(5)},
				Parent:    &CommentRef{ID: 10},
				User:      User{Nickname: "kirk"},
				CreatedOn: created.Add(time.Minute),
			},
		}}))
	}))
	defer srv.Close()

	var got []*bitbucket.ReviewThread
	for thread, err := range newTestGateway(t, srv.URL).
		ListReviewThreads(t.Context(), 7) {
		require.NoError(t, err)
		got = append(got, thread)
	}

	require.Len(t, got, 1)
	assert.Equal(t, &bitbucket.ReviewThread{
		RootCommentID: 10,
		Path:          "review.go",
		Range:         forge.ReviewThreadRange{StartLine: 3, EndLine: 5},
		Side:          forge.ReviewThreadSideLeft,
		Resolved:      true,
		Comments: []bitbucket.ReviewComment{
			{ID: 10, Body: "root", Author: "spock", CreatedAt: created},
			{ID: 11, Body: "reply", Author: "kirk", CreatedAt: created.Add(time.Minute)},
		},
	}, got[0])
}

func TestGateway_ListReviewerStates(t *testing.T) {
	participated := time.Date(2026, time.June, 14, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		require.NoError(t, json.MarshalWrite(w, PullRequest{
			ID: 7,
			Participants: []Participant{
				{User: User{Nickname: "spock"}, State: "approved", ParticipatedOn: &participated},
				{User: User{Nickname: "kirk"}, State: "changes_requested", ParticipatedOn: &participated},
				{User: User{Nickname: "uhura"}, ParticipatedOn: &participated},
				{User: User{Nickname: "bones"}},
			},
		}))
	}))
	defer srv.Close()

	var got []*bitbucket.ReviewerState
	for state, err := range newTestGateway(t, srv.URL).
		ListReviewerStates(t.Context(), 7) {
		require.NoError(t, err)
		got = append(got, state)
	}

	assert.Equal(t, []*bitbucket.ReviewerState{
		{Reviewer: "spock", Disposition: forge.ReviewDispositionApprove, SubmittedAt: participated},
		{Reviewer: "kirk", Disposition: forge.ReviewDispositionRequestChanges, SubmittedAt: participated},
	}, got)
}

func TestGateway_ReviewActions(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	require.NoError(t, gw.SetReviewDisposition(
		t.Context(), 7, forge.ReviewDispositionApprove))
	require.NoError(t, gw.SetReviewDisposition(
		t.Context(), 7, forge.ReviewDispositionRequestChanges))
	require.NoError(t, gw.ResolveReviewThread(t.Context(), 7, 10))
	require.NoError(t, gw.UnresolveReviewThread(t.Context(), 7, 10))

	assert.Equal(t, []string{
		"POST /repositories/workspace/repo/pullrequests/7/approve",
		"POST /repositories/workspace/repo/pullrequests/7/request-changes",
		"POST /repositories/workspace/repo/pullrequests/7/comments/10/resolve",
		"DELETE /repositories/workspace/repo/pullrequests/7/comments/10/resolve",
	}, got)
}

func TestGateway_SetReviewDisposition_none(t *testing.T) {
	assert.Panics(t, func() {
		_ = newTestGateway(t, "http://example.com").SetReviewDisposition(
			t.Context(), 7, forge.ReviewDispositionNone)
	})
}

package server

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/git"
)

func TestGateway_ReviewCapabilities(t *testing.T) {
	tests := []struct {
		name  string
		props map[string]any
		want  bitbucket.ReviewCapabilities
	}{
		{
			name:  "BeforeReviewWorkflow",
			props: map[string]any{"version": "7.6.9"},
			want:  bitbucket.ReviewCapabilities{},
		},
		{
			name:  "ReviewWorkflow",
			props: map[string]any{"version": "7.7.0"},
			want: bitbucket.ReviewCapabilities{
				Supported:    true,
				NativeDrafts: true,
				FileLevel:    true,
			},
		},
		{
			name:  "ThreadResolution",
			props: map[string]any{"version": "8.9.0"},
			want: bitbucket.ReviewCapabilities{
				Supported:        true,
				NativeDrafts:     true,
				FileLevel:        true,
				ThreadResolution: true,
			},
		},
		{
			name:  "BeforeMultiline",
			props: map[string]any{"version": "9.1.9"},
			want: bitbucket.ReviewCapabilities{
				Supported:        true,
				NativeDrafts:     true,
				FileLevel:        true,
				ThreadResolution: true,
			},
		},
		{
			name:  "Multiline",
			props: map[string]any{"version": "9.2.0"},
			want: bitbucket.ReviewCapabilities{
				Supported:        true,
				NativeDrafts:     true,
				FileLevel:        true,
				Multiline:        true,
				ThreadResolution: true,
			},
		},
		{
			name:  "BuildNumberFallback",
			props: map[string]any{"version": "custom", "buildNumber": "9002000"},
			want: bitbucket.ReviewCapabilities{
				Supported:        true,
				NativeDrafts:     true,
				FileLevel:        true,
				Multiline:        true,
				ThreadResolution: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/rest/api/1.0/application-properties", r.URL.Path)
				gatewayWriteJSON(t, w, http.StatusOK, tt.props)
			}))
			defer srv.Close()

			got, err := newOpsTestServerGateway(t, srv).ReviewCapabilities(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGateway_CreateReviewComment_fileLevel(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.UnmarshalRead(r.Body, &got))
		gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{"id": 101})
	}))
	defer srv.Close()

	_, err := newOpsTestServerGateway(t, srv).CreateReviewComment(
		t.Context(), 7, bitbucket.CreateReviewCommentRequest{
			Path: "internal/review.go",
			Side: forge.ReviewThreadSide(99),
			Body: "whole file",
			ReviewContext: bitbucket.ReviewContext{
				BaseHash: "base-sha",
				HeadHash: "head-sha",
			},
		})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"text":  "whole file",
		"state": "PENDING",
		"anchor": map[string]any{
			"diffType": "RANGE",
			"fromHash": "base-sha",
			"path":     "internal/review.go",
			"srcPath":  "internal/review.go",
			"toHash":   "head-sha",
		},
	}, got)
}

func TestGateway_ReviewAnchor_fileLevel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("file-level anchor must not inspect the diff: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	anchor, err := newOpsTestServerGateway(t, srv).ReviewAnchor(
		t.Context(), 7,
		bitbucket.ReviewContext{BaseHash: "base-sha", HeadHash: "head-sha"},
		"internal/review.go", forge.ReviewThreadRange{}, forge.ReviewThreadSide(99),
	)
	require.NoError(t, err)
	assert.Zero(t, anchor)
}

func TestGateway_CreateReviewComment_multilineAnchor(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, prItemPath(7)+"/comments", r.URL.Path)
		require.NoError(t, json.UnmarshalRead(r.Body, &got))
		gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{
			"id": 101, "text": "explain this", "createdDate": 1234,
		})
	}))
	defer srv.Close()

	comment, err := newOpsTestServerGateway(t, srv).CreateReviewComment(
		t.Context(), 7, bitbucket.CreateReviewCommentRequest{
			Path:  "internal/review.go",
			Range: forge.ReviewThreadRange{StartLine: 12, EndLine: 14},
			Side:  forge.ReviewThreadSideLeft,
			Body:  "explain this",
			ReviewContext: bitbucket.ReviewContext{
				BaseHash: git.Hash("base-sha"),
				HeadHash: git.Hash("head-sha"),
			},
			ReviewAnchor: bitbucket.ReviewAnchor{
				StartLineType: "CONTEXT",
				EndLineType:   "REMOVED",
			},
		})
	require.NoError(t, err)
	assert.Equal(t, int64(101), comment.ID)
	assert.Equal(t, map[string]any{
		"text":  "explain this",
		"state": "PENDING",
		"anchor": map[string]any{
			"diffType": "RANGE",
			"fileType": "FROM",
			"fromHash": "base-sha",
			"line":     float64(14),
			"lineType": "REMOVED",
			"multilineMarker": map[string]any{
				"startLine":     float64(12),
				"startLineType": "CONTEXT",
			},
			"path":   "internal/review.go",
			"toHash": "head-sha",
		},
	}, got)
}

func TestGateway_ReviewAnchor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, prItemPath(7)+"/diff/internal/review.go", r.URL.Path)
		assert.Equal(t, "RANGE", r.URL.Query().Get("diffType"))
		assert.Equal(t, "base-sha", r.URL.Query().Get("sinceId"))
		assert.Equal(t, "head-sha", r.URL.Query().Get("untilId"))
		assert.Equal(t, "false", r.URL.Query().Get("withComments"))
		gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
			"hunks": []any{map[string]any{
				"segments": []any{
					map[string]any{
						"type":  "CONTEXT",
						"lines": []any{map[string]any{"source": 10, "destination": 12}},
					},
					map[string]any{
						"type":  "ADDED",
						"lines": []any{map[string]any{"destination": 14}},
					},
				},
			}},
		})
	}))
	defer srv.Close()

	anchor, err := newOpsTestServerGateway(t, srv).ReviewAnchor(
		t.Context(),
		7,
		bitbucket.ReviewContext{BaseHash: "base-sha", HeadHash: "head-sha"},
		"internal/review.go",
		forge.ReviewThreadRange{StartLine: 12, EndLine: 14},
		forge.ReviewThreadSideRight,
	)
	require.NoError(t, err)
	assert.Equal(t, bitbucket.ReviewAnchor{
		StartLineType: "CONTEXT",
		EndLineType:   "ADDED",
	}, anchor)
}

func TestGateway_CreateReviewComment_singleLineAnchor(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.UnmarshalRead(r.Body, &got))
		gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{"id": 101})
	}))
	defer srv.Close()

	_, err := newOpsTestServerGateway(t, srv).CreateReviewComment(
		t.Context(), 7, bitbucket.CreateReviewCommentRequest{
			Path:  "review.go",
			Range: forge.ReviewThreadLine(8),
			Side:  forge.ReviewThreadSideRight,
			Body:  "single line",
			ReviewContext: bitbucket.ReviewContext{
				BaseHash: "base-sha",
				HeadHash: "head-sha",
			},
			ReviewAnchor: bitbucket.ReviewAnchor{
				StartLineType: "CONTEXT",
				EndLineType:   "CONTEXT",
			},
		})
	require.NoError(t, err)
	anchor := got["anchor"].(map[string]any)
	assert.Equal(t, "TO", anchor["fileType"])
	assert.Equal(t, "CONTEXT", anchor["lineType"])
	assert.NotContains(t, anchor, "multilineMarker")
}

func TestGateway_CreateReviewComment_reply(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.UnmarshalRead(r.Body, &got))
		gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{
			"id": 102, "text": "reply",
		})
	}))
	defer srv.Close()

	_, err := newOpsTestServerGateway(t, srv).CreateReviewComment(
		t.Context(), 7, bitbucket.CreateReviewCommentRequest{
			ParentID: 101,
			Body:     "reply",
		})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"text":   "reply",
		"state":  "PENDING",
		"parent": map[string]any{"id": float64(101)},
	}, got)
}

func TestGateway_ReviewContextAndPublish(t *testing.T) {
	var gotBody map[string]any
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, prItemPath(7), r.URL.Path)
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"id": 7, "version": 4,
				"fromRef": map[string]any{"latestCommit": "head-sha"},
				"toRef":   map[string]any{"latestCommit": "base-sha"},
			})
		case 2:
			require.Equal(t, http.MethodPut, r.Method)
			require.Equal(t, prItemPath(7)+"/review", r.URL.Path)
			assert.Equal(t, "4", r.URL.Query().Get("version"))
			require.NoError(t, json.UnmarshalRead(r.Body, &gotBody))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %d: %s %s", calls, r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	review, err := gw.ReviewContext(t.Context(), 7)
	require.NoError(t, err)
	assert.Equal(t, bitbucket.ReviewContext{
		BaseHash: git.Hash("base-sha"),
		HeadHash: git.Hash("head-sha"),
		Version:  4,
	}, review)
	require.NoError(t, gw.PublishReview(
		t.Context(), 7, review, "summary",
		forge.ReviewDispositionRequestChanges,
	))
	assert.Equal(t, map[string]any{
		"commentText":       "summary",
		"participantStatus": "needs_work",
	}, gotBody)
}

func TestGateway_PublishReview_dispositionOnly(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, prItemPath(7)+"/review", r.URL.Path)
		assert.Equal(t, "4", r.URL.Query().Get("version"))
		require.NoError(t, json.UnmarshalRead(r.Body, &gotBody))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	require.NoError(t, newOpsTestServerGateway(t, srv).PublishReview(
		t.Context(), 7, bitbucket.ReviewContext{Version: 4}, "",
		forge.ReviewDispositionApprove,
	))
	assert.Equal(t, map[string]any{"participantStatus": "approved"}, gotBody)
}

func TestGateway_PublishReview_none(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, prItemPath(7)+"/review", r.URL.Path)
		assert.Equal(t, "4", r.URL.Query().Get("version"))
		require.NoError(t, json.UnmarshalRead(r.Body, &gotBody))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	require.NoError(t, newOpsTestServerGateway(t, srv).PublishReview(
		t.Context(), 7, bitbucket.ReviewContext{Version: 4}, "summary",
		forge.ReviewDispositionNone,
	))
	assert.Equal(t, map[string]any{"commentText": "summary"}, gotBody)
}

func TestGateway_ListReviewerStates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, prItemPath(7), r.URL.Path)
		gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
			"reviewers": []any{
				map[string]any{
					"user":   map[string]any{"name": "spock"},
					"status": "APPROVED", "lastReviewedCommit": "abc123",
				},
				map[string]any{
					"user":   map[string]any{"name": "kirk"},
					"status": "NEEDS_WORK", "lastReviewedCommit": "def456",
				},
				map[string]any{
					"user":   map[string]any{"name": "uhura"},
					"status": "UNAPPROVED", "lastReviewedCommit": "789abc",
				},
				map[string]any{
					"user":   map[string]any{"name": "sulu"},
					"status": "UNAPPROVED",
				},
			},
		})
	}))
	defer srv.Close()

	var got []*bitbucket.ReviewerState
	for state, err := range newOpsTestServerGateway(t, srv).ListReviewerStates(t.Context(), 7) {
		require.NoError(t, err)
		got = append(got, state)
	}
	assert.Equal(t, []*bitbucket.ReviewerState{
		{Reviewer: "spock", Disposition: forge.ReviewDispositionApprove, CommitHash: git.Hash("abc123")},
		{Reviewer: "kirk", Disposition: forge.ReviewDispositionRequestChanges, CommitHash: git.Hash("def456")},
	}, got)
}

func TestGateway_ListReviewThreads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, prItemPath(7)+"/comments", r.URL.Path)
		gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
			"isLastPage": true,
			"values": []any{
				map[string]any{"id": 1, "text": "general"},
				map[string]any{
					"id": 101, "text": "root", "createdDate": int64(1000),
					"author":         map[string]any{"name": "spock"},
					"threadResolved": true,
					"anchor": map[string]any{
						"fileType": "TO", "line": 14, "lineType": "ADDED",
						"toHash":          "1111111111111111111111111111111111111111",
						"multilineMarker": map[string]any{"startLine": 12, "startLineType": "CONTEXT"},
						"path":            map[string]any{"components": []string{"internal", "review.go"}},
					},
					"comments": []any{map[string]any{
						"id": 102, "text": "reply", "createdDate": int64(2000),
						"author": map[string]any{"name": "kirk"},
					}},
				},
				map[string]any{
					"id": 102, "text": "reply",
					"parent": map[string]any{"id": 101},
					"anchor": map[string]any{
						"fileType": "TO", "line": 14, "lineType": "ADDED",
						"path": map[string]any{"components": []string{"internal", "review.go"}},
					},
				},
			},
		})
	}))
	defer srv.Close()

	var got []*bitbucket.ReviewThread
	for thread, err := range newOpsTestServerGateway(t, srv).ListReviewThreads(t.Context(), 7) {
		require.NoError(t, err)
		got = append(got, thread)
	}
	require.Len(t, got, 1)
	assert.Equal(t, int64(101), got[0].RootCommentID)
	assert.Equal(t, "internal/review.go", got[0].Path)
	assert.Equal(t, forge.ReviewThreadRange{StartLine: 12, EndLine: 14}, got[0].Range)
	assert.Equal(t, forge.ReviewThreadSideRight, got[0].Side)
	assert.Equal(t, "1111111111111111111111111111111111111111", got[0].CommitHash.String())
	assert.True(t, got[0].Resolved)
	require.Len(t, got[0].Comments, 2)
	assert.Equal(t, int64(101), got[0].Comments[0].ID)
	assert.Equal(t, "root", got[0].Comments[0].Body)
	assert.Equal(t, "spock", got[0].Comments[0].Author)
	assert.Equal(t, int64(102), got[0].Comments[1].ID)
	assert.Equal(t, "reply", got[0].Comments[1].Body)
	assert.Equal(t, "kirk", got[0].Comments[1].Author)
}

func TestGateway_ListReviewThreads_fileLevel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
			"values": []any{map[string]any{
				"id": 10, "text": "whole file",
				"anchor": map[string]any{
					"diffType": "RANGE",
					"fromHash": "base-sha",
					"path":     "internal/review.go",
					"srcPath":  "internal/review.go",
					"toHash":   "head-sha",
				},
			}},
			"isLastPage": true,
		})
	}))
	defer srv.Close()

	var got []*bitbucket.ReviewThread
	for thread, err := range newOpsTestServerGateway(t, srv).
		ListReviewThreads(t.Context(), 7) {
		require.NoError(t, err)
		got = append(got, thread)
	}

	require.Len(t, got, 1)
	assert.Equal(t, "internal/review.go", got[0].Path)
	assert.True(t, got[0].Range.IsZero())
	assert.Equal(t, "head-sha", got[0].CommitHash.String())
}

func TestGateway_ListReviewThreads_rejectsMalformedLineAnchor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
			"values": []any{map[string]any{
				"id": 10, "text": "missing line",
				"anchor": map[string]any{
					"diffType": "RANGE",
					"fileType": "TO",
					"fromHash": "base-sha",
					"path":     "internal/review.go",
					"srcPath":  "internal/review.go",
					"toHash":   "head-sha",
				},
			}},
			"isLastPage": true,
		})
	}))
	defer srv.Close()

	var gotErr error
	for _, err := range newOpsTestServerGateway(t, srv).
		ListReviewThreads(t.Context(), 7) {
		gotErr = err
	}
	require.ErrorContains(t, gotErr, "malformed line anchor")
}

func TestGateway_ResolveReviewThread(t *testing.T) {
	var gotBody map[string]any
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, prItemPath(7)+"/comments/101", r.URL.Path)
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"id": 101, "version": 3, "text": "root",
			})
		case 2:
			require.Equal(t, http.MethodPut, r.Method)
			require.Equal(t, prItemPath(7)+"/comments/101", r.URL.Path)
			require.NoError(t, json.UnmarshalRead(r.Body, &gotBody))
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{})
		default:
			t.Fatalf("unexpected request %d: %s %s", calls, r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	require.NoError(t, newOpsTestServerGateway(t, srv).ResolveReviewThread(
		t.Context(), 7, 101))
	assert.Equal(t, map[string]any{
		"text":           "root",
		"version":        float64(3),
		"threadResolved": true,
	}, gotBody)
}

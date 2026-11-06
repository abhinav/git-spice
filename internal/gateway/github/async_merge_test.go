package github

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_MergePullRequestAsync(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/repos/octo/hello/pulls/102/merge-async", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{
			"sha":"abc123",
			"merge_method":"squash",
			"merge_action":"default"
		}`, string(body))
		return restJSONResponse(http.StatusAccepted, `{
			"status":"pending",
			"details":{
				"message":"Merge request accepted.",
				"uuid":"merge-uuid",
				"merge_method":"squash",
				"merge_action":"default",
				"expected_head_sha":"abc123"
			}
		}`), nil
	}))

	result, err := gateway.MergePullRequestAsync(t.Context(), &MergePullRequestAsyncInput{
		Owner:             "octo",
		Repo:              "hello",
		PullRequestNumber: 102,
		ExpectedHeadSHA:   "abc123",
		Method:            MergeMethodSquash,
	})
	require.NoError(t, err)
	assert.Equal(t, AsyncMergeStatusPending, result.Status)
	assert.Equal(t, "Merge request accepted.", result.Message)
	assert.Equal(t, "merge-uuid", result.OperationID)
}

func TestGateway_MergePullRequestAsyncStatuses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantStatus AsyncMergeStatus
		wantID     string
	}{
		{
			name:       "Pending",
			statusCode: http.StatusAccepted,
			body:       `{"status":"pending","details":{"message":"running","uuid":"new-uuid","merge_action":"default"}}`,
			wantStatus: AsyncMergeStatusPending,
			wantID:     "new-uuid",
		},
		{
			name:       "Merged",
			statusCode: http.StatusOK,
			body:       `{"status":"merged","details":{"message":"merged","sha":"deadbeef"}}`,
			wantStatus: AsyncMergeStatusMerged,
		},
		{
			name:       "Enqueued",
			statusCode: http.StatusOK,
			body:       `{"status":"enqueued","details":{"message":"queued"}}`,
			wantStatus: AsyncMergeStatusEnqueued,
		},
		{
			name:       "Failed",
			statusCode: http.StatusBadRequest,
			body:       `{"status":"failed","details":{"message":"pull request is closed"}}`,
			wantStatus: AsyncMergeStatusFailed,
		},
		{
			name:       "ExistingPendingWithDifferentOptions",
			statusCode: http.StatusConflict,
			body:       `{"status":"pending","details":{"message":"already running","uuid":"existing-uuid","expected_head_sha":"other","merge_method":"rebase","merge_action":"direct_merge"}}`,
			wantStatus: AsyncMergeStatusPending,
			wantID:     "existing-uuid",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return restJSONResponse(tt.statusCode, tt.body), nil
			}))

			result, err := gateway.MergePullRequestAsync(t.Context(), &MergePullRequestAsyncInput{
				Owner:             "octo",
				Repo:              "hello",
				PullRequestNumber: 102,
				ExpectedHeadSHA:   "abc123",
				Method:            MergeMethodSquash,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantID, result.OperationID)
		})
	}
}

func TestGateway_MergePullRequestAsyncErrors(t *testing.T) {
	for _, tt := range []struct {
		name       string
		statusCode int
		want       error
	}{
		{name: "NotFound", statusCode: http.StatusNotFound, want: ErrNotFound},
		{name: "Validation", statusCode: http.StatusUnprocessableEntity, want: ErrUnprocessable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return restJSONResponse(tt.statusCode, `{"message":"request rejected"}`), nil
			}))

			_, err := gateway.MergePullRequestAsync(t.Context(), &MergePullRequestAsyncInput{
				Owner:             "octo",
				Repo:              "hello",
				PullRequestNumber: 102,
			})
			assert.ErrorIs(t, err, tt.want)
		})
	}
}

func TestGateway_MergePullRequestAsyncInvalidResult(t *testing.T) {
	t.Run("Status", func(t *testing.T) {
		gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			return restJSONResponse(
				http.StatusAccepted,
				`{"status":"waiting","details":{"uuid":"merge-uuid"}}`,
			), nil
		}))

		_, err := gateway.MergePullRequestAsync(t.Context(), &MergePullRequestAsyncInput{
			Owner:             "octo",
			Repo:              "hello",
			PullRequestNumber: 102,
		})
		assert.ErrorContains(t, err, `unknown asynchronous merge status "waiting"`)
	})

	t.Run("PendingWithoutOperationID", func(t *testing.T) {
		gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			return restJSONResponse(
				http.StatusAccepted,
				`{"status":"pending","details":{"message":"running"}}`,
			), nil
		}))

		_, err := gateway.MergePullRequestAsync(t.Context(), &MergePullRequestAsyncInput{
			Owner:             "octo",
			Repo:              "hello",
			PullRequestNumber: 102,
		})
		assert.ErrorContains(t, err, "pending asynchronous merge has no operation ID")
	})

	t.Run("MergeMethod", func(t *testing.T) {
		gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("HTTP request made with invalid merge method")
			return nil, nil
		}))

		_, err := gateway.MergePullRequestAsync(t.Context(), &MergePullRequestAsyncInput{
			Owner:             "octo",
			Repo:              "hello",
			PullRequestNumber: 102,
			Method:            MergeMethod(100),
		})
		assert.ErrorContains(t, err, "encode merge method: unknown GitHub enum value 100")
	})
}

func TestGateway_AsyncMergeResult(t *testing.T) {
	for _, tt := range []struct {
		name       string
		body       string
		wantStatus AsyncMergeStatus
	}{
		{
			name:       "Pending",
			body:       `{"status":"pending","details":{"message":"running","uuid":"merge-uuid"}}`,
			wantStatus: AsyncMergeStatusPending,
		},
		{
			name:       "Merged",
			body:       `{"status":"merged","details":{"message":"merged","sha":"deadbeef"}}`,
			wantStatus: AsyncMergeStatusMerged,
		},
		{
			name:       "Enqueued",
			body:       `{"status":"enqueued","details":{"message":"queued"}}`,
			wantStatus: AsyncMergeStatusEnqueued,
		},
		{
			name:       "Failed",
			body:       `{"status":"failed","details":{"message":"merge conflict"}}`,
			wantStatus: AsyncMergeStatusFailed,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/repos/octo/hello/pulls/102/merge-async/merge-uuid", r.URL.Path)
				assert.Empty(t, r.Header.Get("Content-Type"))
				return restJSONResponse(http.StatusOK, tt.body), nil
			}))

			result, err := gateway.AsyncMergeResult(
				t.Context(),
				"octo",
				"hello",
				102,
				"merge-uuid",
			)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, result.Status)
		})
	}
}

func TestGateway_AsyncMergeResultNotFound(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return restJSONResponse(http.StatusNotFound, `{"message":"Not Found"}`), nil
	}))

	_, err := gateway.AsyncMergeResult(
		t.Context(),
		"octo",
		"hello",
		102,
		"expired-uuid",
	)
	assert.ErrorIs(t, err, ErrNotFound)
}

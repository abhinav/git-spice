package github

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// V3Client is a GitHub REST API v3 client.
// Exported for use in tests.
type V3Client = githubv3Client

// NewV3Client creates a new GitHub REST API v3 client.
// Exported for use in tests.
var NewV3Client = newGitHubv3Client

func TestGithubv3Error_Error(t *testing.T) {
	tests := []struct {
		name string
		give *githubv3Error
		want string
	}{
		{
			name: "AllFieldsPopulated",
			give: &githubv3Error{
				Resource: "PullRequest",
				Field:    "base",
				Code:     "invalid",
				Message:  "must be a valid branch",
			},
			want: "PullRequest.base: invalid (must be a valid branch)",
		},
		{
			name: "ResourceAndFieldOnly",
			give: &githubv3Error{
				Resource: "Issue",
				Field:    "title",
				Code:     "missing",
			},
			want: "Issue.title: missing",
		},
		{
			name: "ResourceAndMessage",
			give: &githubv3Error{
				Resource: "Repository",
				Message:  "not found",
			},
			want: "Repository: not found",
		},
		{
			name: "FieldAndMessage",
			give: &githubv3Error{
				Field:   "name",
				Message: "is required",
			},
			want: "name: is required",
		},
		{
			name: "MessageOnly",
			give: &githubv3Error{
				Message: "something went wrong",
			},
			want: "something went wrong",
		},
		{
			name: "CustomCode",
			give: &githubv3Error{
				Resource: "PullRequest",
				Field:    "reviewers",
				Code:     "custom",
				Message:  "cannot request review from author",
			},
			want: "PullRequest.reviewers: cannot request review from author",
		},
		{
			name: "CustomCodeNoResourceOrField",
			give: &githubv3Error{
				Code:    "custom",
				Message: "custom validation error",
			},
			want: "custom validation error",
		},
		{
			name: "EmptyCode",
			give: &githubv3Error{
				Resource: "Branch",
				Field:    "name",
				Code:     "",
				Message:  "invalid branch name",
			},
			want: "Branch.name: invalid branch name",
		},
		{
			name: "CodeWithoutMessage",
			give: &githubv3Error{
				Resource: "Label",
				Field:    "color",
				Code:     "invalid",
			},
			want: "Label.color: invalid",
		},
		{
			name: "ResourceOnly",
			give: &githubv3Error{
				Resource: "Milestone",
			},
			want: "Milestone: ",
		},
		{
			name: "FieldOnly",
			give: &githubv3Error{
				Field: "assignees",
			},
			want: "assignees: ",
		},
		{
			name: "CodeOnly",
			give: &githubv3Error{
				Code: "missing_field",
			},
			want: "missing_field",
		},
		{
			name: "AllEmpty",
			give: &githubv3Error{},
			want: "",
		},
		{
			name: "ResourceFieldAndCode",
			give: &githubv3Error{
				Resource: "Issue",
				Field:    "labels",
				Code:     "already_exists",
			},
			want: "Issue.labels: already_exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.give.Error()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGithubv3ResponseError_Error(t *testing.T) {
	tests := []struct {
		name string
		give *githubv3ResponseError
		want string
	}{
		{
			name: "BasicError",
			give: &githubv3ResponseError{
				StatusCode: 404,
				Message:    "Not Found",
			},
			want: "GitHub API error (status 404): Not Found",
		},
		{
			name: "ErrorWithDocumentation",
			give: &githubv3ResponseError{
				StatusCode:       422,
				Message:          "Validation Failed",
				DocumentationURL: "https://docs.github.com/rest/pulls",
			},
			want: joinLines(
				"GitHub API error (status 422): Validation Failed",
				"For more information, see: https://docs.github.com/rest/pulls",
			),
		},
		{
			name: "ErrorWithSingleDetailedError",
			give: &githubv3ResponseError{
				StatusCode: 422,
				Message:    "Validation Failed",
				Errors: []*githubv3Error{
					{
						Resource: "PullRequest",
						Field:    "base",
						Code:     "invalid",
						Message:  "must be a valid branch",
					},
				},
			},
			want: joinLines(
				"GitHub API error (status 422): Validation Failed",
				"  - PullRequest.base: invalid (must be a valid branch)",
			),
		},
		{
			name: "ErrorWithMultipleDetailedErrors",
			give: &githubv3ResponseError{
				StatusCode: 422,
				Message:    "Validation Failed",
				Errors: []*githubv3Error{
					{
						Resource: "Issue",
						Field:    "title",
						Code:     "missing",
					},
					{
						Resource: "Issue",
						Field:    "body",
						Code:     "missing",
					},
				},
			},
			want: joinLines(
				"GitHub API error (status 422): Validation Failed",
				"  - Issue.title: missing",
				"  - Issue.body: missing",
			),
		},
		{
			name: "ErrorWithAllFields",
			give: &githubv3ResponseError{
				StatusCode: 422,
				Message:    "Validation Failed",
				Errors: []*githubv3Error{
					{
						Resource: "PullRequest",
						Field:    "reviewers",
						Code:     "custom",
						Message:  "cannot request review from author",
					},
				},
				DocumentationURL: "https://docs.github.com/rest/pulls#request-reviewers",
			},
			want: joinLines(
				"GitHub API error (status 422): Validation Failed",
				"  - PullRequest.reviewers: cannot request review from author",
				"For more information, see: https://docs.github.com/rest/pulls#request-reviewers",
			),
		},
		{
			name: "EmptyMessage",
			give: &githubv3ResponseError{
				StatusCode: 500,
				Message:    "",
			},
			want: "GitHub API error (status 500): ",
		},
		{
			name: "ZeroStatusCode",
			give: &githubv3ResponseError{
				StatusCode: 0,
				Message:    "Unknown error",
			},
			want: "GitHub API error (status 0): Unknown error",
		},
		{
			name: "EmptyErrorsSlice",
			give: &githubv3ResponseError{
				StatusCode: 400,
				Message:    "Bad Request",
				Errors:     []*githubv3Error{},
			},
			want: "GitHub API error (status 400): Bad Request",
		},
		{
			name: "NilErrorsSlice",
			give: &githubv3ResponseError{
				StatusCode: 403,
				Message:    "Forbidden",
				Errors:     nil,
			},
			want: "GitHub API error (status 403): Forbidden",
		},
		{
			name: "ErrorWithEmptyDetailedError",
			give: &githubv3ResponseError{
				StatusCode: 422,
				Message:    "Validation Failed",
				Errors: []*githubv3Error{
					{},
				},
			},
			want: joinLines(
				"GitHub API error (status 422): Validation Failed",
				"  - ",
			),
		},
		{
			name: "ErrorWithMessageOnlyDetailedError",
			give: &githubv3ResponseError{
				StatusCode: 422,
				Message:    "Validation Failed",
				Errors: []*githubv3Error{
					{
						Message: "string error message",
					},
				},
			},
			want: joinLines(
				"GitHub API error (status 422): Validation Failed",
				"  - string error message",
			),
		},
		{
			name: "ErrorWithMixedDetailedErrors",
			give: &githubv3ResponseError{
				StatusCode: 422,
				Message:    "Validation Failed",
				Errors: []*githubv3Error{
					{
						Resource: "PullRequest",
						Field:    "base",
						Code:     "invalid",
					},
					{
						Message: "plain string error",
					},
					{
						Resource: "Issue",
						Code:     "custom",
						Message:  "custom validation",
					},
				},
				DocumentationURL: "https://docs.github.com/rest",
			},
			want: joinLines(
				"GitHub API error (status 422): Validation Failed",
				"  - PullRequest.base: invalid",
				"  - plain string error",
				"  - Issue: custom validation",
				"For more information, see: https://docs.github.com/rest",
			),
		},
		{
			name: "LargeStatusCode",
			give: &githubv3ResponseError{
				StatusCode: 999,
				Message:    "Unexpected error",
			},
			want: "GitHub API error (status 999): Unexpected error",
		},
		{
			name: "EmptyDocumentationURL",
			give: &githubv3ResponseError{
				StatusCode:       404,
				Message:          "Not Found",
				DocumentationURL: "",
			},
			want: "GitHub API error (status 404): Not Found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.give.Error()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGithubv3Error_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		give string
		want *githubv3Error
	}{
		{
			name: "StructuredError",
			give: `{"resource":"PullRequest","field":"base","code":"invalid","message":"must be a valid branch"}`,
			want: &githubv3Error{
				Resource: "PullRequest",
				Field:    "base",
				Code:     "invalid",
				Message:  "must be a valid branch",
			},
		},
		{
			name: "PlainStringError",
			give: `"plain error message"`,
			want: &githubv3Error{
				Message: "plain error message",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got githubv3Error
			err := json.Unmarshal([]byte(tt.give), &got)
			require.NoError(t, err)
			assert.Equal(t, tt.want, &got)
		})
	}
}

func TestGithubv3Client_Do(t *testing.T) {
	clientForServer := func(t *testing.T, handler func(http.ResponseWriter, *http.Request)) *githubv3Client {
		srv := httptest.NewServer(http.HandlerFunc(handler))
		t.Cleanup(srv.Close)

		u, err := url.Parse(srv.URL)
		require.NoError(t, err)
		return NewV3Client(srv.Client(), u)
	}

	t.Run("NoRequestBody", func(t *testing.T) {
		client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/repos/owner/repo", r.URL.Path)
			assert.Empty(t, r.Header.Get("Content-Type"))

			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.Empty(t, body)

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id": 123}`))
		})

		var res struct {
			ID int `json:"id"`
		}
		_, err := client.Do(t.Context(), http.MethodGet, "/repos/owner/repo", nil, &res)
		require.NoError(t, err)
		assert.Equal(t, 123, res.ID)
	})

	t.Run("NoResponseBody", func(t *testing.T) {
		client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/repos/owner/repo/issues", r.URL.Path)

			var reqBody struct {
				Title string `json:"title"`
			}
			err := json.NewDecoder(r.Body).Decode(&reqBody)
			require.NoError(t, err)
			assert.Equal(t, "test issue", reqBody.Title)

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id": 456}`))
		})

		reqBody := struct {
			Title string `json:"title"`
		}{
			Title: "test issue",
		}
		_, err := client.Do(t.Context(), http.MethodPost, "/repos/owner/repo/issues", reqBody, nil)
		require.NoError(t, err)
	})

	t.Run("RequestBody", func(t *testing.T) {
		client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/repos/owner/repo/pulls", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			var reqBody struct {
				Title string `json:"title"`
				Head  string `json:"head"`
				Base  string `json:"base"`
			}
			err := json.NewDecoder(r.Body).Decode(&reqBody)
			require.NoError(t, err)
			assert.Equal(t, "test pr", reqBody.Title)
			assert.Equal(t, "feature", reqBody.Head)
			assert.Equal(t, "main", reqBody.Base)

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number": 789}`))
		})

		reqBody := struct {
			Title string `json:"title"`
			Head  string `json:"head"`
			Base  string `json:"base"`
		}{
			Title: "test pr",
			Head:  "feature",
			Base:  "main",
		}
		var resBody struct {
			Number int `json:"number"`
		}
		_, err := client.Do(t.Context(), http.MethodPost, "/repos/owner/repo/pulls", reqBody, &resBody)
		require.NoError(t, err)
		assert.Equal(t, 789, resBody.Number)
	})

	t.Run("ResponseBody", func(t *testing.T) {
		client := clientForServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": 999,
				"title": "test issue",
				"state": "open",
				"labels": ["bug", "enhancement"]
			}`))
		})

		var resBody struct {
			ID     int      `json:"id"`
			Title  string   `json:"title"`
			State  string   `json:"state"`
			Labels []string `json:"labels"`
		}
		_, err := client.Do(t.Context(), http.MethodGet, "/repos/owner/repo/issues/999", nil, &resBody)
		require.NoError(t, err)
		assert.Equal(t, 999, resBody.ID)
		assert.Equal(t, "test issue", resBody.Title)
		assert.Equal(t, "open", resBody.State)
		assert.Equal(t, []string{"bug", "enhancement"}, resBody.Labels)
	})

	t.Run("Non2xxStatusCode", func(t *testing.T) {
		client := clientForServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{
				"message": "Not Found",
				"documentation_url": "https://docs.github.com/rest"
			}`))
		})

		_, err := client.Do(t.Context(), http.MethodGet, "/repos/owner/nonexistent", nil, nil)
		assert.Error(t, err)

		var ghErr *githubv3ResponseError
		require.ErrorAs(t, err, &ghErr)
		assert.Equal(t, http.StatusNotFound, ghErr.StatusCode)
		assert.Equal(t, "Not Found", ghErr.Message)
		assert.Equal(t, "https://docs.github.com/rest", ghErr.DocumentationURL)
	})

	t.Run("JSONErrorResponse", func(t *testing.T) {
		client := clientForServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{
				"message": "Validation Failed",
				"errors": [
					{
						"resource": "PullRequest",
						"field": "base",
						"code": "invalid",
						"message": "must be a valid branch"
					}
				],
				"documentation_url": "https://docs.github.com/rest/pulls"
			}`))
		})

		_, err := client.Do(t.Context(), http.MethodPost, "/repos/owner/repo/pulls", nil, nil)
		assert.Error(t, err)

		var ghErr *githubv3ResponseError
		require.ErrorAs(t, err, &ghErr)
		assert.Equal(t, &githubv3ResponseError{
			StatusCode: http.StatusUnprocessableEntity,
			Message:    "Validation Failed",
			Errors: []*githubv3Error{
				{
					Resource: "PullRequest",
					Field:    "base",
					Code:     "invalid",
					Message:  "must be a valid branch",
				},
			},
			DocumentationURL: "https://docs.github.com/rest/pulls",
		}, ghErr)
	})

	t.Run("NonJSONErrorResponse", func(t *testing.T) {
		client := clientForServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal Server Error"))
		})

		_, err := client.Do(t.Context(), http.MethodGet, "/repos/owner/repo", nil, nil)
		assert.Error(t, err)

		var ghErr *githubv3ResponseError
		require.ErrorAs(t, err, &ghErr)
		assert.Equal(t, &githubv3ResponseError{
			StatusCode: http.StatusInternalServerError,
			Message:    "Internal Server Error",
		}, ghErr)
	})

	t.Run("StatusNoContent", func(t *testing.T) {
		client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/repos/owner/repo/git/refs/heads/branch", r.URL.Path)

			w.WriteHeader(http.StatusNoContent)
		})

		resp, err := client.Do(t.Context(), http.MethodDelete, "/repos/owner/repo/git/refs/heads/branch", nil, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func joinLines(lines ...string) string {
	return strings.Join(lines, "\n")
}

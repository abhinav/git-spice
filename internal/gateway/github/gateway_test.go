package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGateway_endpoint(t *testing.T) {
	tests := []struct {
		name string
		give string
		want string
	}{
		{name: "GitHub", give: "https://api.github.com", want: "https://api.github.com/graphql"},
		{name: "Enterprise", give: "https://github.example.com/api", want: "https://github.example.com/api/graphql"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway, err := NewGateway(tt.give, nil, tokenSourceFunc(func(context.Context) (string, error) {
				return "token", nil
			}))
			require.NoError(t, err)
			assert.Equal(t, tt.want, gateway.endpoint)
		})
	}
}

func TestGateway_RepositoryID_request(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "github.example.com", r.URL.Host)
		assert.Equal(t, "/api/graphql", r.URL.Path)
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{
			"query":"query($owner:String!$repo:String!){repository(owner: $owner, name: $repo){id}}",
			"variables":{"owner":"octo","repo":"hello"}
		}`, string(body))
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(
				`{"data":{"repository":{"id":"R_123"}}}`,
			)),
		}, nil
	})

	var tokenCalls int
	gateway, err := NewGateway("https://github.example.com/api", &http.Client{Transport: transport}, tokenSourceFunc(func(ctx context.Context) (string, error) {
		assert.Same(t, t.Context(), ctx)
		tokenCalls++
		return "secret", nil
	}))
	require.NoError(t, err)

	id, err := gateway.RepositoryID(t.Context(), "octo", "hello")
	require.NoError(t, err)
	assert.Equal(t, ID("R_123"), id)
	assert.Equal(t, 1, tokenCalls)
}

func TestGateway_RepositoryID_tokenError(t *testing.T) {
	want := errors.New("token unavailable")
	gateway, err := NewGateway("https://api.github.com", nil, tokenSourceFunc(func(context.Context) (string, error) {
		return "", want
	}))
	require.NoError(t, err)

	_, err = gateway.RepositoryID(t.Context(), "octo", "hello")
	assert.ErrorIs(t, err, want)
}

func TestGateway_RepositoryID_cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	gateway, err := NewGateway("https://api.github.com", nil, tokenSourceFunc(func(ctx context.Context) (string, error) {
		return "", ctx.Err()
	}))
	require.NoError(t, err)

	_, err = gateway.RepositoryID(ctx, "octo", "hello")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestGateway_RepositoryID_httpError(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxErrorBody+100))),
		}, nil
	}))

	_, err := gateway.RepositoryID(t.Context(), "octo", "hello")
	require.Error(t, err)
	assert.ErrorContains(t, err, "502 Bad Gateway")
	assert.Less(t, len(err.Error()), maxErrorBody+200)
}

func TestGateway_RepositoryID_responseErrors(t *testing.T) {
	t.Run("MalformedJSON", func(t *testing.T) {
		gateway := newResponseGateway(t, `{`)
		_, err := gateway.RepositoryID(t.Context(), "octo", "hello")
		assert.ErrorContains(t, err, "decode GraphQL response")
	})

	t.Run("ReadFailure", func(t *testing.T) {
		want := errors.New("read failure")
		gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       errorReader{err: want},
			}, nil
		}))
		_, err := gateway.RepositoryID(t.Context(), "octo", "hello")
		assert.ErrorIs(t, err, want)
	})
}

func TestGateway_RepositoryID_graphQLErrors(t *testing.T) {
	tests := []struct {
		name string
		give string
		want error
	}{
		{
			name: "One",
			give: `{"errors":[{"message":"missing","path":["repository"],"type":"NOT_FOUND"}]}`,
			want: ErrNotFound,
		},
		{
			name: "Multiple",
			give: `{"errors":[{"message":"denied","type":"FORBIDDEN"},{"message":"invalid","type":"UNPROCESSABLE"}]}`,
			want: ErrForbidden,
		},
		{
			name: "DataAndErrors",
			give: `{"data":{"repository":{"id":"R_123"}},"errors":[{"message":"denied","type":"FORBIDDEN"}]}`,
			want: ErrForbidden,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := newResponseGateway(t, tt.give)
			id, err := gateway.RepositoryID(t.Context(), "octo", "hello")
			assert.Empty(t, id)
			assert.ErrorIs(t, err, tt.want)

			var gqlErrors graphQLError
			require.ErrorAs(t, err, &gqlErrors)
			assert.NotEmpty(t, gqlErrors)
		})
	}
}

func TestGateway_CreatePullRequest_optionalInputs(t *testing.T) {
	tests := []struct {
		name  string
		input CreatePullRequestInput
		want  string
	}{
		{
			name: "Omitted",
			input: CreatePullRequestInput{
				RepositoryID: "R_1",
				BaseRefName:  "main",
				HeadRefName:  "topic",
				Title:        "Title",
			},
			want: `{"repositoryId":"R_1","baseRefName":"main","headRefName":"topic","title":"Title"}`,
		},
		{
			name: "Present",
			input: CreatePullRequestInput{
				RepositoryID:     "R_1",
				BaseRefName:      "main",
				HeadRefName:      "octo:topic",
				Title:            "Title",
				HeadRepositoryID: new(ID("R_2")),
				Body:             new(""),
				Draft:            new(false),
			},
			want: `{"repositoryId":"R_1","baseRefName":"main","headRefName":"octo:topic","title":"Title","headRepositoryId":"R_2","body":"","draft":false}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var request struct {
					Query     string `json:"query"`
					Variables struct {
						Input json.RawMessage `json:"input"`
					} `json:"variables"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				assert.Equal(t, "mutation($input:CreatePullRequestInput!){createPullRequest(input: $input){pullRequest{id,number,url}}}", request.Query)
				assert.JSONEq(t, tt.want, string(request.Variables.Input))
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":{"createPullRequest":{"pullRequest":{"id":"PR_1","number":1,"url":"https://example.com/1"}}}}`))}, nil
			}))
			_, err := gateway.CreatePullRequest(t.Context(), &tt.input)
			require.NoError(t, err)
		})
	}
}

func TestGateway_MergePullRequest_optionalInputs(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{
			"query":"mutation($input:MergePullRequestInput!){mergePullRequest(input: $input){pullRequest{id}}}",
			"variables":{"input":{"pullRequestId":"PR_1","expectedHeadOid":"abc","mergeMethod":"SQUASH"}}
		}`, string(body))
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":{"mergePullRequest":{"pullRequest":{"id":"PR_1"}}}}`))}, nil
	}))
	err := gateway.MergePullRequest(t.Context(), &MergePullRequestInput{
		PullRequestID:   "PR_1",
		ExpectedHeadOID: new("abc"),
		MergeMethod:     new(MergeMethodSquash),
	})
	require.NoError(t, err)
}

func newResponseGateway(t *testing.T, body string) *Gateway {
	t.Helper()
	return newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}))
}

func newTestGateway(t *testing.T, transport http.RoundTripper) *Gateway {
	t.Helper()
	gateway, err := NewGateway("https://api.github.com", &http.Client{Transport: transport}, tokenSourceFunc(func(context.Context) (string, error) {
		return "token", nil
	}))
	require.NoError(t, err)
	return gateway
}

type tokenSourceFunc func(context.Context) (string, error)

func (f tokenSourceFunc) Token(ctx context.Context) (string, error) { return f(ctx) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }
func (errorReader) Close() error               { return nil }

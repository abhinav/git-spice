package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_RESTAuthenticationRefresh(t *testing.T) {
	var tokenCalls int
	gateway, err := NewGateway(
		"https://api.github.com",
		&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			assert.Equal(t, fmt.Sprintf("Bearer token-%d", tokenCalls), r.Header.Get("Authorization"))
			return restJSONResponse(http.StatusCreated, `{}`), nil
		})},
		tokenSourceFunc(func(ctx context.Context) (string, error) {
			assert.Same(t, t.Context(), ctx)
			tokenCalls++
			return fmt.Sprintf("token-%d", tokenCalls), nil
		}),
	)
	require.NoError(t, err)

	for range 2 {
		err := gateway.CreatePullRequestStack(t.Context(), &CreatePullRequestStackInput{
			Owner:        "octo",
			Repo:         "hello",
			PullRequests: []int{101, 102},
		})
		require.NoError(t, err)
	}
	assert.Equal(t, 2, tokenCalls)
}

func TestGateway_RESTTokenError(t *testing.T) {
	want := errors.New("token unavailable")
	gateway, err := NewGateway(
		"https://api.github.com",
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("HTTP request made after token failure")
			return nil, nil
		})},
		tokenSourceFunc(func(context.Context) (string, error) {
			return "", want
		}),
	)
	require.NoError(t, err)

	err = gateway.CreatePullRequestStack(t.Context(), &CreatePullRequestStackInput{
		Owner:        "octo",
		Repo:         "hello",
		PullRequests: []int{101, 102},
	})
	assert.ErrorIs(t, err, want)
}

func TestGateway_RESTErrorClassification(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       error
	}{
		{name: "Forbidden", statusCode: http.StatusForbidden, want: ErrForbidden},
		{name: "NotFound", statusCode: http.StatusNotFound, want: ErrNotFound},
		{name: "Unprocessable", statusCode: http.StatusUnprocessableEntity, want: ErrUnprocessable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return restJSONResponse(tt.statusCode, `{"message":"request rejected"}`), nil
			}))

			err := gateway.CreatePullRequestStack(t.Context(), &CreatePullRequestStackInput{
				Owner:        "octo",
				Repo:         "hello",
				PullRequests: []int{101, 102},
			})
			assert.ErrorIs(t, err, tt.want)

			var restErr *githubRESTError
			require.ErrorAs(t, err, &restErr)
			assert.Equal(t, tt.statusCode, restErr.statusCode)
			assert.Contains(t, restErr.diagnostic, "request rejected")
		})
	}
}

func TestGateway_RESTErrorBoundedDiagnostic(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return restJSONResponse(
			http.StatusUnprocessableEntity,
			strings.Repeat("x", maxErrorBody+100),
		), nil
	}))

	err := gateway.CreatePullRequestStack(t.Context(), &CreatePullRequestStackInput{
		Owner:        "octo",
		Repo:         "hello",
		PullRequests: []int{101, 102},
	})
	var restErr *githubRESTError
	require.ErrorAs(t, err, &restErr)
	assert.Len(t, restErr.diagnostic, maxErrorBody)
	assert.Less(t, len(err.Error()), maxErrorBody+250)
}

func TestGateway_RESTErrorReadFailurePreservesClassification(t *testing.T) {
	want := errors.New("read failure")
	gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       errorReader{err: want},
		}, nil
	}))

	err := gateway.CreatePullRequestStack(t.Context(), &CreatePullRequestStackInput{
		Owner:        "octo",
		Repo:         "hello",
		PullRequests: []int{101, 102},
	})
	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, err, want)
}

func TestGateway_RESTResponseReadFailure(t *testing.T) {
	want := errors.New("read failure")
	gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusCreated,
			Body:       errorReader{err: want},
		}, nil
	}))

	err := gateway.CreatePullRequestStack(t.Context(), &CreatePullRequestStackInput{
		Owner:        "octo",
		Repo:         "hello",
		PullRequests: []int{101, 102},
	})
	assert.ErrorIs(t, err, want)
}

func restJSONResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

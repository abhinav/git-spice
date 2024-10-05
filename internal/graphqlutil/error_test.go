package graphqlutil_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/graphqlutil"
)

func TestResponseError(t *testing.T) {
	res, err := (&http.Client{
		Transport: graphqlutil.WrapTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return nil, assert.AnError
		})),
	}).Get("http://example.com")
	require.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
	assert.Nil(t, res) // non-nil res, non-nil err is not allowed
}

func TestReadError(t *testing.T) {
	_, err := (&http.Client{
		Transport: graphqlutil.WrapTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(iotest.ErrReader(assert.AnError)),
			}, nil
		})),
	}).Get("http://example.com")
	require.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestResponseStatusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	res, err := (&http.Client{
		Transport: graphqlutil.WrapTransport(http.DefaultTransport),
	}).Get(srv.URL)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
}

func TestMalformedResponse(t *testing.T) {
	const give = `{
		"data": null,
		"errors": [{"got as far as this but no further
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, give)
	}))
	defer srv.Close()

	res, err := (&http.Client{
		Transport: graphqlutil.WrapTransport(http.DefaultTransport),
	}).Get(srv.URL)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()

	bs, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Equal(t, give, string(bs))
}

func TestGraphQLResponse(t *testing.T) {
	tests := []struct {
		name string
		body string

		wantErrorIs  []error
		wantErrorAs  []any
		wantContains []string
	}{
		{
			name: "no errors",
			body: `{"data":{}, "errors":[]}`,
		},
		{
			name: "not found",
			body: `{
				"data": {"repository": null},
				"errors": [
					{
						"type": "NOT_FOUND",
						"path": ["repository"],
						"locations": [
							{"line": 2, "column": 3}
						],
						"message": "Could not resolve to a Repository with the name 'foo/bar'."
					}
				]
			}`,
			wantErrorIs: []error{
				graphqlutil.ErrNotFound,
			},
			wantErrorAs: []any{
				new(graphqlutil.ErrorList),
				new(*graphqlutil.Error),
			},
		},
		{
			name: "forbidden",
			body: `{
				"data": {"repository": null},
				"errors": [
					{
						"type": "FORBIDDEN",
						"path": ["repository"],
						"locations": [
							{"line": 2, "column": 3}
						],
						"message": "Permission denied."
					}
				]
			}`,
			wantErrorIs: []error{
				graphqlutil.ErrForbidden,
			},
			wantErrorAs: []any{
				new(graphqlutil.ErrorList),
				new(*graphqlutil.Error),
			},
		},
		{
			name: "multiple errors",
			body: `{
				"data": {"repository": null},
				"errors": [
					{
						"type": "NOT_FOUND",
						"path": ["repository"],
						"locations": [
							{"line": 2, "column": 3}
						],
						"message": "Could not resolve to a Repository with the name 'foo/bar'."
					},
					{
						"type": "FORBIDDEN",
						"path": ["repository"],
						"locations": [
							{"line": 2, "column": 3}
						],
						"message": "Permission denied."
					}
				]
			}`,
			wantErrorIs: []error{
				graphqlutil.ErrForbidden,
				graphqlutil.ErrNotFound,
			},
			wantErrorAs: []any{
				new(graphqlutil.ErrorList),
				new(*graphqlutil.Error),
			},
		},
		{
			name: "unrecognized error",
			body: `{
				"data": {"repository": null},
				"errors": [
					{
						"type": "UNKNOWN",
						"path": ["repository"],
						"locations": [
							{"line": 2, "column": 3}
						],
						"message": "lol"
					}
				]
			}`,
			wantErrorAs: []any{
				new(graphqlutil.ErrorList),
				new(*graphqlutil.Error),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			wantErr := len(tt.wantErrorIs) > 0 ||
				len(tt.wantErrorAs) > 0 ||
				len(tt.wantContains) > 0

			res, err := (&http.Client{
				Transport: graphqlutil.WrapTransport(http.DefaultTransport),
			}).Get(srv.URL)
			if !wantErr {
				require.NoError(t, err)

				defer func() {
					assert.NoError(t, res.Body.Close())
				}()

				bs, err := io.ReadAll(res.Body)
				require.NoError(t, err)
				assert.Equal(t, tt.body, string(bs))

				return
			}

			require.Error(t, err)
			// Sanity check: error must always errors.Is itself.
			assert.ErrorIs(t, err, err)
			for _, wantErr := range tt.wantErrorIs {
				assert.ErrorIs(t, err, wantErr)
			}
			for _, wantErr := range tt.wantErrorAs {
				assert.ErrorAs(t, err, wantErr)
			}
			for _, wantContains := range tt.wantContains {
				assert.ErrorContains(t, err, wantContains)
			}
		})
	}
}

func TestErrorString(t *testing.T) {
	tests := []struct {
		name string
		give *graphqlutil.Error
		want string
	}{
		{
			name: "simple",
			give: &graphqlutil.Error{
				Type:    "NOT_FOUND",
				Path:    []any{"repository"},
				Message: "Could not resolve",
			},
			want: `repository: NOT_FOUND: Could not resolve`,
		},
		{
			name: "NoPath",
			give: &graphqlutil.Error{
				Type:    "NOT_FOUND",
				Message: "Could not resolve",
			},
			want: `NOT_FOUND: Could not resolve`,
		},
		{
			name: "NoType",
			give: &graphqlutil.Error{
				Path:    []any{"repository"},
				Message: "Could not resolve",
			},
			want: `repository: Could not resolve`,
		},
		{
			name: "OnlyMessage",
			give: &graphqlutil.Error{
				Message: "Could not resolve",
			},
			want: `Could not resolve`,
		},
		{
			name: "MultiplePath",
			give: &graphqlutil.Error{
				Path:    []any{"repository", "owner"},
				Message: "Could not resolve",
			},
			want: `repository.owner: Could not resolve`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.Error())
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

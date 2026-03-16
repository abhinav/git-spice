package github

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestCommentCountsByChange(t *testing.T) {
	t.Run("NoPagination", func(t *testing.T) {
		srv := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				enc := json.NewEncoder(w)
				assert.NoError(t, enc.Encode(map[string]any{
					"data": map[string]any{
						"nodes": []map[string]any{
							{
								"reviewThreads": map[string]any{
									"totalCount": 2,
									"pageInfo": map[string]any{
										"endCursor":   nil,
										"hasNextPage": false,
									},
									"nodes": []map[string]any{
										{"isResolved": true},
										{"isResolved": false},
									},
								},
							},
						},
					},
				}))
			}),
		)
		defer srv.Close()

		repo, err := newRepository(
			t.Context(), new(Forge),
			"owner", "repo",
			silogtest.New(t),
			githubv4.NewEnterpriseClient(srv.URL, nil),
			"repoID",
		)
		require.NoError(t, err)

		counts, err := repo.CommentCountsByChange(
			t.Context(),
			[]forge.ChangeID{
				&PR{Number: 1, GQLID: "prID1"},
			},
		)
		require.NoError(t, err)
		require.Len(t, counts, 1)
		assert.Equal(t, 2, counts[0].Total)
		assert.Equal(t, 1, counts[0].Resolved)
		assert.Equal(t, 1, counts[0].Unresolved)
	})

	t.Run("WithPagination", func(t *testing.T) {
		requestNum := 0
		srv := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				query := string(body)

				enc := json.NewEncoder(w)

				switch {
				case strings.Contains(query, "nodes(ids:"):
					// Batch query: return first page
					// with hasNextPage=true.
					requestNum++
					assert.NoError(t, enc.Encode(map[string]any{
						"data": map[string]any{
							"nodes": []map[string]any{
								{
									"reviewThreads": map[string]any{
										"totalCount": 3,
										"pageInfo": map[string]any{
											"endCursor":   "cursor1",
											"hasNextPage": true,
										},
										"nodes": []map[string]any{
											{"isResolved": true},
											{"isResolved": false},
										},
									},
								},
							},
						},
					}))

				case strings.Contains(query, "node(id:"):
					// Pagination query:
					// return remaining threads.
					requestNum++
					assert.NoError(t, enc.Encode(map[string]any{
						"data": map[string]any{
							"node": map[string]any{
								"reviewThreads": map[string]any{
									"totalCount": 3,
									"pageInfo": map[string]any{
										"endCursor":   nil,
										"hasNextPage": false,
									},
									"nodes": []map[string]any{
										{"isResolved": true},
									},
								},
							},
						},
					}))

				default:
					t.Fatalf("unexpected query: %s", query)
				}
			}),
		)
		defer srv.Close()

		repo, err := newRepository(
			t.Context(), new(Forge),
			"owner", "repo",
			silogtest.New(t),
			githubv4.NewEnterpriseClient(srv.URL, nil),
			"repoID",
		)
		require.NoError(t, err)

		counts, err := repo.CommentCountsByChange(
			t.Context(),
			[]forge.ChangeID{
				&PR{Number: 1, GQLID: "prID1"},
			},
		)
		require.NoError(t, err)
		require.Len(t, counts, 1)
		assert.Equal(t, 3, counts[0].Total)
		assert.Equal(t, 2, counts[0].Resolved)
		assert.Equal(t, 1, counts[0].Unresolved)

		// Verify both the batch and pagination queries
		// were made.
		assert.Equal(t, 2, requestNum)
	})
}

package azuredevops

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConnection_bearerAuthentication(t *testing.T) {
	connection := newConnection(
		"https://dev.azure.com/example",
		AuthenticationBearer,
		"token",
	)

	assert.Equal(t, "Bearer token", connection.AuthorizationString)
}

func TestNewGateway_httpClientDiscovery(t *testing.T) {
	for _, empty := range []bool{false, true} {
		name := "DiscoveredServices"
		if empty {
			name = "OnPremises"
		}
		t.Run(name, func(t *testing.T) {
			var paths []string
			var baseURL string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodOptions {
					_, _ = w.Write([]byte(`{"value":[
      {"id":"e81700f7-3be2-46de-8624-2eb35882fcaa","area":"","resourceName":"resourceAreas","routeTemplate":"_apis/resourceAreas","resourceVersion":1,"minVersion":"1.0","maxVersion":"7.1","releasedVersion":"7.1"},
      {"id":"225f7195-f9c7-4d14-ab28-a83f7ff77e1f","area":"","resourceName":"repositories","routeTemplate":"repository","resourceVersion":1,"minVersion":"1.0","maxVersion":"7.1","releasedVersion":"7.1"},
      {"id":"28010c54-d0c0-4c89-a5b0-1c9e188b9fb7","area":"","resourceName":"identities","routeTemplate":"identities","resourceVersion":1,"minVersion":"1.0","maxVersion":"7.1","releasedVersion":"7.1"}
     ]}`))
					return
				}
				if r.URL.Path == "/_apis/resourceAreas" {
					areas := []map[string]string{}
					if !empty {
						areas = append(areas,
							map[string]string{"id": "4e080c62-fa21-4fbc-8fef-2a10a2b38049", "locationUrl": baseURL + "/git"},
							map[string]string{"id": "8a3d49b8-91f0-46ef-b33d-dda338c25db3", "locationUrl": baseURL + "/identity"})
					}
					_ = json.MarshalWrite(w, map[string]any{"value": areas})
					return
				}
				switch r.URL.Path {
				case "/git/repository", "/repository":
					_, _ = w.Write([]byte(`{"name":"repository"}`))
				case "/identity/identities", "/identities":
					_, _ = w.Write([]byte(`{"value":[{"id":"00000000-0000-4000-8000-000000000999"}]}`))
				default:
					http.Error(w, "unexpected request", http.StatusBadRequest)
				}
			}))
			defer server.Close()
			baseURL = server.URL
			gateway, err := NewGateway(t.Context(), baseURL, AuthenticationBearer, "token", &Options{HTTPClient: server.Client()})
			require.NoError(t, err)
			repo, err := gateway.Repository(t.Context(), "project", "repository")
			require.NoError(t, err)
			assert.Equal(t, "repository", repo.Name)
			reviewer, err := gateway.ReviewerID(t.Context(), "reviewer")
			require.NoError(t, err)
			assert.Equal(t, "00000000-0000-4000-8000-000000000999", reviewer)
			require.Contains(t, paths, "/_apis/resourceAreas")
			if !empty {
				assert.Contains(t, paths, "/git/_apis")
				assert.Contains(t, paths, "/identity/identities")
			} else {
				assert.Contains(t, paths, "/repository")
				assert.Contains(t, paths, "/identities")
			}
		})
	}
}

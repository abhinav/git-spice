package shamhub

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/ui"
)

func TestForge_AuthenticationFlow_httpClient(t *testing.T) {
	t.Setenv("SHAMHUB_USERNAME", "user")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/login", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		_, _ = w.Write([]byte(`{"token":"token"}`))
	}))
	defer server.Close()
	f := &Forge{URL: server.URL, APIURL: server.URL, HTTPClient: server.Client()}
	token, err := f.AuthenticationFlow(t.Context(), ui.NewFileView(io.Discard))
	require.NoError(t, err)
	assert.Equal(t, "token", token.(*AuthenticationToken).tok)
}

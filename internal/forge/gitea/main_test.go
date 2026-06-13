package gitea

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain intercepts the test binary when invoked as "git"
// to fake git credential fill for tests that need it.
func TestMain(m *testing.M) {
	name := filepath.Base(os.Args[0])
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	}

	if name == "git" {
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func writeJSON(t *testing.T, w http.ResponseWriter, code int, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

func assertJSONBody(t *testing.T, r *http.Request, want string) {
	t.Helper()

	var body any
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

	got, err := json.Marshal(body)
	require.NoError(t, err)
	assert.JSONEq(t, want, string(got))
}

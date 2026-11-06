package forgetest

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/httptest"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// NewHTTPRecorder creates a new HTTP recorder for the given test and name.
// Sanitizers are applied to recorded fixtures in update mode.
func NewHTTPRecorder(
	t *testing.T,
	name string,
	sanitizers []httptest.Sanitizer,
) *recorder.Recorder {
	return httptest.NewTransportRecorder(t, name, httptest.TransportRecorderOptions{
		Update:     Update,
		Sanitizers: sanitizers,
		Matcher: func(r *http.Request, i cassette.Request) bool {
			// If there's no body, just match the method and URL.
			if r.Body == nil || r.Body == http.NoBody {
				return r.Method == i.Method && r.URL.String() == i.URL
			}

			reqBody, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.NoError(t, r.Body.Close())

			r.Body = io.NopCloser(bytes.NewBuffer(reqBody))

			// Trim trailing newlines for comparison.
			// YAML block scalars add a trailing newline to the body.
			actualBody := strings.TrimRight(string(reqBody), "\n")
			expectedBody := strings.TrimRight(i.Body, "\n")

			return r.Method == i.Method &&
				r.URL.String() == i.URL &&
				actualBody == expectedBody
		},
	})
}

// Package httptest provides utilities for HTTP testing.
// It includes helpers for the VCR library we use.
package httptest

import (
	"maps"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// TransportRecorderOptions contains options for creating a new
// [TransportRecorder].
type TransportRecorderOptions struct {
	// Update specifies whether the Recorder should update fixtures.
	//
	// If unset, we will assume false.
	Update *bool

	// WrapRealTransport wraps the real HTTP transport
	// with the given function.
	//
	// This is called only in update mode.
	WrapRealTransport func(
		t testing.TB,
		transport http.RoundTripper,
	) http.RoundTripper

	Matcher func(*http.Request, cassette.Request) bool
}

// NewTransportRecorder builds a new HTTP request recorder/replayer
// that will write fixtures to testdata/fixtures/<name>.
//
// The returned Recorder will be in recording mode if the -update flag is set,
// and in replay mode otherwise.
func NewTransportRecorder(
	t testing.TB,
	name string,
	opts TransportRecorderOptions,
) *recorder.Recorder {
	t.Helper()

	mode := recorder.ModeReplayOnly
	realTransport := http.DefaultTransport
	afterCaptureHook := func(*cassette.Interaction) error {
		return nil
	}

	if opts.Update != nil && *opts.Update {
		mode = recorder.ModeRecordOnly
		if opts.WrapRealTransport != nil {
			realTransport = opts.WrapRealTransport(t, realTransport)
		}

		// Paranoid mode:
		// maintain an allowlist of headers to keep in the fixtures
		// so as not to accidentally record sensitive data.
		afterCaptureHook = func(i *cassette.Interaction) error {
			allHeaders := make(http.Header)
			maps.Copy(allHeaders, i.Request.Headers)
			maps.Copy(allHeaders, i.Response.Headers)

			var toRemove []string
			for k := range allHeaders {
				switch strings.ToLower(k) {
				case "content-type", "content-length", "user-agent", "x-next-page", "x-total-pages", "x-page":
					// ok
				default:
					toRemove = append(toRemove, k)
				}
			}

			for _, k := range toRemove {
				delete(i.Request.Headers, k)
				delete(i.Response.Headers, k)
			}

			return nil
		}
	}

	matcher := cassette.DefaultMatcher
	if opts.Matcher != nil {
		matcher = opts.Matcher
	}

	rec, err := recorder.New(filepath.Join("testdata", "fixtures", name),
		recorder.WithMode(mode),
		recorder.WithRealTransport(realTransport),
		recorder.WithSkipRequestLatency(true),
		recorder.WithHook(afterCaptureHook, recorder.AfterCaptureHook),
		recorder.WithMatcher(matcher),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, rec.Stop())
	})

	return rec
}

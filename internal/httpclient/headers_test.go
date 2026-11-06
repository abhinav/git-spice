package httpclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOrigin(t *testing.T) {
	tests := []struct {
		name string
		give string
		want string
	}{
		{
			name: "DefaultPort",
			give: "https://EXAMPLE.com:443/",
			want: "https://example.com",
		},
		{
			name: "ExplicitPort",
			give: "https://example.com:8443",
			want: "https://example.com:8443",
		},
		{
			name: "IPv6",
			give: "https://[::1]:443",
			want: "https://[::1]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origin, err := ParseOrigin(tt.give)
			require.NoError(t, err)

			assert.Equal(t, tt.want, origin.String())
		})
	}
}

func TestParseOrigin_invalid(t *testing.T) {
	tests := []struct {
		name string
		give string
	}{
		{
			name: "HTTP",
			give: "http://example.com",
		},
		{
			name: "Path",
			give: "https://example.com/api",
		},
		{
			name: "Query",
			give: "https://example.com?",
		},
		{
			name: "Fragment",
			give: "https://example.com#fragment",
		},
		{
			name: "UserInfo",
			give: "https://user:password@example.com",
		},
		{
			name: "MissingHost",
			give: "https://",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseOrigin(tt.give)

			require.Error(t, err)
		})
	}
}

func TestHeaderTransport_RoundTrip_redirects(t *testing.T) {
	for _, scheme := range []string{"HTTPS", "HTTP"} {
		t.Run(scheme, func(t *testing.T) {
			var got struct {
				initialRequest          http.Header
				sameOriginRedirect      http.Header
				otherConfiguredOrigin   http.Header
				unconfiguredDestination http.Header
			}
			destination := httptest.NewUnstartedServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					got.unconfiguredDestination = r.Header.Clone()
					_, _ = io.WriteString(w, "ok")
				},
			))
			if scheme == "HTTPS" {
				destination.StartTLS()
			} else {
				destination.Start()
			}
			defer destination.Close()

			second := httptest.NewTLSServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					got.otherConfiguredOrigin = r.Header.Clone()
					http.Redirect(w, r, destination.URL, http.StatusFound)
				},
			))
			defer second.Close()

			first := httptest.NewTLSServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/" {
						got.initialRequest = r.Header.Clone()
						http.Redirect(w, r, "/next", http.StatusFound)
						return
					}
					got.sameOriginRedirect = r.Header.Clone()
					http.Redirect(w, r, second.URL, http.StatusFound)
				},
			))
			defer first.Close()

			rules := []HeaderRule{
				headerRuleForTest(t, first.URL, "X-Access-Token", "first"),
				headerRuleForTest(t, first.URL, "X-Only-First", "first-only"),
				headerRuleForTest(t, second.URL, "X-Access-Token", "second"),
				headerRuleForTest(t, second.URL, "X-Only-Second", "second-only"),
			}
			transport, err := NewHeaderTransport(first.Client().Transport, rules)
			require.NoError(t, err)

			client := &http.Client{Transport: transport}
			req, err := http.NewRequestWithContext(
				t.Context(), http.MethodGet, first.URL, nil,
			)
			require.NoError(t, err)
			req.Header.Set("User-Agent", "test")

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer func() { assert.NoError(t, resp.Body.Close()) }()

			require.NotNil(t, got.initialRequest)
			assert.Equal(t, "first", got.initialRequest.Get("X-Access-Token"))
			assert.Equal(t, "first-only", got.initialRequest.Get("X-Only-First"))

			require.NotNil(t, got.sameOriginRedirect)
			assert.Equal(t, "first", got.sameOriginRedirect.Get("X-Access-Token"))

			require.NotNil(t, got.otherConfiguredOrigin)
			assert.Equal(t, "second", got.otherConfiguredOrigin.Get("X-Access-Token"))
			assert.Empty(t, got.otherConfiguredOrigin.Get("X-Only-First"))
			assert.Equal(t, "second-only",
				got.otherConfiguredOrigin.Get("X-Only-Second"))

			require.NotNil(t, got.unconfiguredDestination)
			assert.Empty(t, got.unconfiguredDestination.Get("X-Access-Token"))
			assert.Empty(t, got.unconfiguredDestination.Get("X-Only-First"))
			assert.Empty(t, got.unconfiguredDestination.Get("X-Only-Second"))

			assert.Equal(t, http.Header{"User-Agent": []string{"test"}}, req.Header)
		})
	}
}

func TestHeaderTransport_RoundTrip_matching(t *testing.T) {
	rule := headerRuleForTest(t, "https://example.com", "X-Access-Token", "secret")
	tests := []struct {
		name string
		give string
		want string
	}{
		{
			name: "SameOrigin",
			give: "https://example.com/api",
			want: "secret",
		},
		{
			name: "DefaultPort",
			give: "https://EXAMPLE.COM:443/elsewhere",
			want: "secret",
		},
		{
			name: "Subdomain",
			give: "https://other.example.com",
		},
		{
			name: "HTTP",
			give: "http://example.com",
		},
		{
			name: "OtherPort",
			give: "https://example.com:8443",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := &headerCaptureTransport{}
			transport, err := NewHeaderTransport(capture, []HeaderRule{rule})
			require.NoError(t, err)

			req, err := http.NewRequestWithContext(
				t.Context(), http.MethodGet, tt.give, nil,
			)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer forge-token")

			_, err = transport.RoundTrip(req)
			require.NoError(t, err)

			assert.Equal(t, tt.want, capture.header.Get("X-Access-Token"))
			assert.Equal(t, "Bearer forge-token", capture.header.Get("Authorization"))
			assert.Empty(t, req.Header.Get("X-Access-Token"))
		})
	}
}

func TestNewHeaderTransport_duplicateHeader(t *testing.T) {
	_, err := NewHeaderTransport(nil, []HeaderRule{
		headerRuleForTest(t, "https://EXAMPLE.com:443", "X-Token", "secret-one"),
		headerRuleForTest(t, "https://example.com/", "x-token", "secret-two"),
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")
}

func TestHeaderTransport_RoundTrip_existingHeader(t *testing.T) {
	capture := &headerCaptureTransport{}
	transport, err := NewHeaderTransport(capture, []HeaderRule{
		headerRuleForTest(t, "https://example.com", "Authorization", "configured-secret"),
	})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodGet, "https://example.com", nil,
	)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer forge-secret")

	_, err = transport.RoundTrip(req)
	require.NoError(t, err)

	assert.Equal(t, "configured-secret", capture.header.Get("Authorization"))
	assert.Equal(t, "Bearer forge-secret", req.Header.Get("Authorization"))
}

func headerRuleForTest(t *testing.T, rawOrigin, name, value string) HeaderRule {
	t.Helper()
	origin, err := ParseOrigin(rawOrigin)
	require.NoError(t, err)
	rule, err := NewHeaderRule(origin, name, value)
	require.NoError(t, err)
	return rule
}

type headerCaptureTransport struct {
	header http.Header
}

func (c *headerCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.header = req.Header.Clone()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       http.NoBody,
	}, nil
}

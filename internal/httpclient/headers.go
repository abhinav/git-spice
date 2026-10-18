// Package httpclient supplies destination-scoped HTTP request policy.
package httpclient

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// HeaderTransport adds configured headers to cloned requests before sending
// them through its underlying transport.
// It does not modify requests owned by the HTTP client, so injected headers
// do not enter the client's redirect-header copying policy.
type HeaderTransport struct {
	base            http.RoundTripper
	headersByOrigin map[originKey]map[string]string
}

var _ http.RoundTripper = (*HeaderTransport)(nil)

// NewHeaderTransport builds a transport, rejecting duplicate configured header
// names at the same normalized origin.
// A nil base uses [http.DefaultTransport].
func NewHeaderTransport(base http.RoundTripper, rules []HeaderRule) (*HeaderTransport, error) {
	headersByOrigin := make(map[originKey]map[string]string)
	for _, rule := range rules {
		key := originKeyForURL(&rule.origin.url)
		headers, ok := headersByOrigin[key]
		if !ok {
			headers = make(map[string]string)
			headersByOrigin[key] = headers
		}
		if _, ok := headers[rule.name]; ok {
			return nil, fmt.Errorf("duplicate HTTP header %q for %s", rule.name, rule.origin)
		}
		headers[rule.name] = rule.value
	}
	return &HeaderTransport{
		base:            cmp.Or(base, http.DefaultTransport),
		headersByOrigin: headersByOrigin,
	}, nil
}

// RoundTrip applies only the rules for this request's destination.
// Configured headers replace existing values with the same name.
func (t *HeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.EqualFold(req.URL.Scheme, "https") {
		return t.base.RoundTrip(req)
	}
	headers, ok := t.headersByOrigin[originKeyForURL(req.URL)]
	if !ok {
		return t.base.RoundTrip(req)
	}
	outgoing := req.Clone(req.Context())
	if outgoing.Header == nil {
		outgoing.Header = make(http.Header)
	}
	for name, value := range headers {
		outgoing.Header.Set(name, value)
	}
	return t.base.RoundTrip(outgoing)
}

// CloseIdleConnections closes idle connections in the underlying transport
// when it supports that operation.
func (t *HeaderTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type originKey struct {
	hostname string
	port     string
}

func originKeyForURL(u *url.URL) originKey {
	return originKey{hostname: strings.ToLower(u.Hostname()), port: cmp.Or(u.Port(), "443")}
}

// HeaderRule supplies one header value to requests at an exact HTTPS origin.
// Values are resolved before construction and are never expanded by a transport.
type HeaderRule struct {
	origin Origin
	name   string
	value  string
}

// NewHeaderRule builds a header rule for the supplied origin.
// Header names and values are validated by net/http when sending a request.
func NewHeaderRule(origin Origin, name, value string) (HeaderRule, error) {
	if origin.url.Hostname() == "" {
		return HeaderRule{}, errors.New("header origin is required")
	}
	return HeaderRule{origin: origin, name: http.CanonicalHeaderKey(name), value: value}, nil
}

// Origin identifies one HTTPS hostname and effective port.
// Construct origins with [ParseOrigin].
type Origin struct {
	url url.URL
}

// ParseOrigin accepts an HTTPS URL with no user information, query, fragment,
// or path other than an optional trailing slash.
// Hostnames are case-insensitive and the default port is 443.
func ParseOrigin(raw string) (Origin, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Origin{}, errors.New("invalid HTTPS origin")
	}
	switch {
	case !strings.EqualFold(u.Scheme, "https"):
		return Origin{}, errors.New("expected an HTTPS origin")
	case u.Opaque != "":
		return Origin{}, errors.New("HTTPS origin must not be opaque")
	case u.Hostname() == "":
		return Origin{}, errors.New("HTTPS origin requires a hostname")
	case u.User != nil:
		return Origin{}, errors.New("HTTPS origin must not contain credentials")
	case (u.Path != "" && u.Path != "/") || u.RawPath != "":
		return Origin{}, errors.New("HTTPS origin must not contain a path other than a trailing slash")
	case u.RawQuery != "" || u.ForceQuery:
		return Origin{}, errors.New("HTTPS origin must not contain a query")
	case u.Fragment != "" || strings.Contains(raw, "#"):
		return Origin{}, errors.New("HTTPS origin must not contain a fragment")
	}
	u.Scheme = "https"
	u.Host = strings.ToLower(u.Host)
	if u.Port() == "443" {
		u.Host = strings.TrimSuffix(u.Host, ":443")
	}
	u.Path = ""
	return Origin{url: *u}, nil
}

func (o Origin) String() string {
	return o.url.String()
}

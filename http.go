package main

import (
	"net/http"

	"go.abhg.dev/gs/internal/httpclient"
	"go.abhg.dev/gs/internal/spice"
)

// newHTTPClient resolves application HTTP settings once.
// A nil base uses the default transport.
func newHTTPClient(config *spice.Config, base http.RoundTripper) (*http.Client, error) {
	rules, err := config.HTTPHeaderRules()
	if err != nil {
		return nil, err
	}
	transport, err := httpclient.NewHeaderTransport(base, rules)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: transport}, nil
}

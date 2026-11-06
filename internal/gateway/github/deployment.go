package github

import (
	"cmp"
	"fmt"
	"net/url"
	"strings"
)

// Deployment configures the addresses of a GitHub installation.
// The zero value selects GitHub.com.
// API addresses are resolved when constructing a [Gateway].
type Deployment struct {
	// URL is the web URL. If empty, https://github.com is used.
	URL string

	// APIURL overrides the common base URL for the GraphQL and REST APIs.
	// For GitHub Enterprise Server, this normally ends in /api,
	// before the REST API's /v3 suffix.
	// If empty, GitHub.com uses its public API host,
	// and custom web URLs use an /api subpath.
	APIURL string
}

// WebURL returns the configured web URL or the GitHub.com default.
// It does not parse or validate either address.
func (d Deployment) WebURL() string {
	return cmp.Or(d.URL, defaultWebURL)
}

type apiEndpoints struct {
	GraphQLEndpoint *url.URL
	RESTBaseURL     *url.URL
}

// apiEndpoints resolves the common API base into privately owned protocol URLs.
// It returns an error if an explicit APIURL cannot be parsed.
func (d Deployment) apiEndpoints() (apiEndpoints, error) {
	apiURL := &url.URL{Scheme: "https", Host: defaultAPIHost}
	switch {
	case d.APIURL != "":
		var err error
		apiURL, err = url.Parse(d.APIURL)
		if err != nil {
			return apiEndpoints{}, fmt.Errorf("parse API URL: %w", err)
		}
	case d.URL != "" && d.URL != defaultWebURL:
		// Retain the API default if the web URL cannot be parsed.
		// Web operations validate their own URL.
		if webURL, err := url.Parse(d.URL); err == nil {
			apiURL = webURL.JoinPath("/api")
		}
	}

	// GitHub.com and GHE.com serve GraphQL below the REST API host,
	// while GitHub Enterprise Server uses /api/graphql and /api/v3.
	apiHost := strings.ToLower(apiURL.Hostname())
	isGitHubCloud := apiHost == defaultAPIHost ||
		(strings.HasPrefix(apiHost, "api.") && strings.HasSuffix(apiHost, ".ghe.com"))
	restBaseURL := apiURL
	if !isGitHubCloud {
		restBaseURL = apiURL.JoinPath("v3")
	}

	return apiEndpoints{
		GraphQLEndpoint: apiURL.JoinPath("graphql"),
		RESTBaseURL:     restBaseURL,
	}, nil
}

const (
	defaultWebURL  = "https://github.com"
	defaultAPIHost = "api.github.com"
)

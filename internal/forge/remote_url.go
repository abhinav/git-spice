package forge

import (
	"fmt"
	"net/url"
	"strings"

	"go.abhg.dev/gs/internal/git/giturl"
)

// ValidateRemoteURL verifies that remoteURL can be used with configuredURL.
//
// The configured URL is the provider URL supplied by configuration, flags,
// or environment variables.
// It may be empty when the provider uses its built-in default base URL.
//
// Validation requires a non-nil remote URL.
// If configuredURL is set, validation also requires the remote host to match
// the configured URL host or one of its subdomains.
// If configuredURL includes a port, the remote URL must use the same port.
func ValidateRemoteURL(configuredURL string, remoteURL *giturl.URL) error {
	if remoteURL == nil {
		return fmt.Errorf("%w: remote URL is required", ErrUnsupportedURL)
	}
	if configuredURL == "" {
		return nil
	}
	if remoteURLMatches(configuredURL, remoteURL) {
		return nil
	}
	return fmt.Errorf("%w: remote URL %q does not match configured forge URL %q",
		ErrUnsupportedURL, remoteURL.Raw, configuredURL)
}

func remoteURLMatches(forgeURL string, remoteURL *giturl.URL) bool {
	if remoteURL == nil {
		return false
	}

	baseURL, err := url.Parse(forgeURL)
	if err != nil {
		return false
	}

	baseHost := baseURL.Hostname()
	remoteHost := remoteURL.Hostname
	// Some forges advertise a base URL such as "https://github.com",
	// while Git remotes use a related SSH hostname like "ssh.github.com".
	// Accept subdomains so these documented SSH hosts still infer
	// the same forge.
	hostMatches := remoteHost == baseHost ||
		strings.HasSuffix(remoteHost, "."+baseHost)
	if !hostMatches {
		return false
	}

	// A base URL without an explicit port describes the forge host,
	// not one transport endpoint.
	// In that case, allow the remote to specify its SSH port.
	basePort := baseURL.Port()
	return basePort == "" || remoteURL.Port == basePort
}

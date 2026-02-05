// Package forgeurl provides shared URL parsing utilities for forge implementations.
package forgeurl

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// _gitProtocols is a list of known git protocols including the :// suffix.
var _gitProtocols []string

func init() {
	protocols := []string{
		"ssh",
		"git",
		"git+ssh",
		"git+https",
		"git+http",
		"https",
		"http",
	}
	_gitProtocols = make([]string, len(protocols))
	for i, proto := range protocols {
		_gitProtocols[i] = proto + "://"
	}
}

// HasGitProtocol reports whether the URL starts with a known git protocol.
func HasGitProtocol(rawURL string) bool {
	for _, proto := range _gitProtocols {
		if strings.HasPrefix(rawURL, proto) {
			return true
		}
	}
	return false
}

// Parse parses a git remote URL, normalizing SSH shorthand syntax.
//
// It converts SCP-style URLs (git@host:path) to standard SSH URLs
// (ssh://git@host/path) before parsing.
func Parse(rawURL string) (*url.URL, error) {
	if !HasGitProtocol(rawURL) && strings.Contains(rawURL, ":") {
		rawURL = "ssh://" + strings.Replace(rawURL, ":", "/", 1)
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse remote URL: %w", err)
	}
	return u, nil
}

// StripDefaultPort removes default HTTP/HTTPS ports (80, 443) from the
// remote URL's host when the base URL doesn't explicitly specify a port.
func StripDefaultPort(baseURL, remoteURL *url.URL) {
	if baseURL.Port() != "" {
		return
	}
	host, port, err := net.SplitHostPort(remoteURL.Host)
	if err != nil {
		return
	}
	if port == "443" || port == "80" {
		remoteURL.Host = host
	}
}

// MatchesHost reports whether the remote URL's host matches the base URL's
// host, either exactly or as a subdomain.
func MatchesHost(baseURL, remoteURL *url.URL) bool {
	if remoteURL.Host == baseURL.Host {
		return true
	}
	return strings.HasSuffix(remoteURL.Host, "."+baseURL.Host)
}

// ExtractPath extracts owner and repository name from a URL path.
//
// It strips leading/trailing slashes and the .git suffix, then splits
// on the first slash to get owner/repo components.
func ExtractPath(path string) (owner, repo string, ok bool) {
	s := strings.TrimPrefix(path, "/")
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")

	owner, repo, ok = strings.Cut(s, "/")
	return owner, repo, ok
}

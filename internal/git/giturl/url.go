// Package giturl parses Git repository remote URLs.
package giturl

import (
	"fmt"
	"net/url"
	"strings"
)

// URL is the normalized view of a Git repository remote URL.
//
// It is parsed at the Git boundary so forge resolution code can compare
// remote hosts without depending on Git's raw URL syntaxes.
// For example,
// "git@github.com:owner/repo.git" parses to Hostname "github.com"
// and Path "/owner/repo.git".
type URL struct {
	// Raw is the original remote URL before normalization.
	Raw string

	// Scheme is the parsed remote URL scheme after normalization.
	// SCP-style SSH shorthand normalizes to "ssh".
	Scheme string

	// Path is the repository path,
	// including the leading slash and any ".git" suffix.
	// For example,
	// "ssh://git@github.com/owner/repo.git" has Path "/owner/repo.git".
	Path string

	// Hostname is the remote host without any port.
	// For example,
	// "ssh://git@ssh.github.com:443/owner/repo.git"
	// has Hostname "ssh.github.com".
	Hostname string

	// Port is the remote port,
	// or empty if the remote URL does not specify one.
	Port string
}

// Parse parses a Git repository remote URL.
//
// It accepts standard URL forms like
// "ssh://git@github.com/owner/repo.git" and Git's SCP-style SSH shorthand
// like "git@github.com:owner/repo.git".
func Parse(raw string) (*URL, error) {
	normalized := raw
	if !hasGitProtocol(normalized) && strings.Contains(normalized, ":") {
		normalized = "ssh://" + strings.Replace(normalized, ":", "/", 1)
	}

	u, err := url.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("parse remote URL: %w", err)
	}

	return &URL{
		Raw:      raw,
		Scheme:   u.Scheme,
		Path:     u.Path,
		Hostname: u.Hostname(),
		Port:     u.Port(),
	}, nil
}

func hasGitProtocol(raw string) bool {
	protocols := []string{
		"ssh",
		"git",
		"git+ssh",
		"git+https",
		"git+http",
		"https",
		"http",
	}
	for _, proto := range protocols {
		if strings.HasPrefix(raw, proto+"://") {
			return true
		}
	}
	return false
}

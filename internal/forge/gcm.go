package forge

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/xec"
)

// GCMCredential holds credentials retrieved from git-credential-manager.
type GCMCredential struct {
	// Username is the account identifier (may be empty).
	Username string

	// Password is the access token or password.
	Password string
}

// LoadGCMCredential loads credentials from git-credential-manager
// for the given URL. Returns an error if GCM is not available
// or has no credentials for the host.
func LoadGCMCredential(ctx context.Context, forgeURL string) (*GCMCredential, error) {
	host := extractHost(forgeURL)
	input := fmt.Sprintf("protocol=https\nhost=%s\n\n", host)

	output, err := xec.Command(ctx, nil, "git", "credential", "fill").
		WithStdinString(input).
		Output()
	if err != nil {
		return nil, fmt.Errorf("git credential fill: %w", err)
	}

	return parseCredentialOutput(output)
}

// parseCredentialOutput parses the output of `git credential fill`.
// The format is key=value pairs, one per line.
func parseCredentialOutput(output []byte) (*GCMCredential, error) {
	var cred GCMCredential

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}

		switch key {
		case "username":
			cred.Username = value
		case "password":
			cred.Password = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse credential output: %w", err)
	}

	if cred.Password == "" {
		return nil, errors.New("no password in credential output")
	}

	return &cred, nil
}

// extractHost extracts the host from a URL.
func extractHost(rawURL string) string {
	// Remove protocol prefix.
	host := rawURL
	if idx := strings.Index(host, "://"); idx != -1 {
		host = host[idx+3:]
	}

	// Remove path suffix.
	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}
	return host
}

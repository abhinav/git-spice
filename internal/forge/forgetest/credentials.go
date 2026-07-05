package forgetest

import (
	"encoding/json"
	"os"
	"testing"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
)

// Canonical placeholders for test repository values in VCR fixtures.
// These make fixtures portable across different test environments.
const (
	CanonicalOwner = "test-owner"
	CanonicalRepo  = "test-repo"
)

// Token retrieves authentication credentials for the given forge URL.
// In update mode, it tries multiple sources in order:
//  1. Environment variable (explicit override)
//  2. Stored OAuth credentials from secret stash (from 'gs auth login')
//  3. GCM (git-credential-manager)
//
// In replay mode, it returns a dummy token.
//
// The envVar parameter should be the name of the environment variable
// to check (e.g., "GITHUB_TOKEN").
func Token(t *testing.T, forgeURL, envVar string) string {
	if !Update() {
		return "token"
	}

	// Try environment variable first for explicit override.
	if token := os.Getenv(envVar); token != "" {
		t.Logf("Using %s from environment", envVar)
		return token
	}

	// Try stored OAuth credentials from stash.
	if token := loadStashToken(t, forgeURL); token != "" {
		t.Logf("Using stored OAuth token from stash for %s", forgeURL)
		return token
	}

	// Try GCM.
	cred, err := forge.LoadGCMCredential(t.Context(), forgeURL)
	if err == nil {
		t.Logf("Using token from git-credential-manager for %s", forgeURL)
		return cred.Password
	}

	t.Fatalf("No credentials available for %s: set %s, run 'gs auth login', or configure git-credential-manager",
		forgeURL, envVar)
	return ""
}

// loadStashToken attempts to load OAuth credentials from the secret stash.
// Returns empty string if no credentials are found or on error.
func loadStashToken(t *testing.T, forgeURL string) string {
	stash := new(secret.Keyring)
	tokstr, err := stash.LoadSecret(forgeURL, "token")
	if err != nil {
		t.Logf("load stored token for %s: %v", forgeURL, err)
		return ""
	}

	// Parse the JSON token structure to extract access_token.
	// Fall back to the raw string for old token formats.
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal([]byte(tokstr), &tok); err != nil {
		return tokstr
	}

	return tok.AccessToken
}

// CredentialSource indicates where credentials were obtained from.
type CredentialSource int

const (
	// CredentialSourceEnv indicates credentials from environment variables.
	// These are typically API tokens using Bearer auth.
	CredentialSourceEnv CredentialSource = iota

	// CredentialSourceGCM indicates credentials
	// from git-credential-manager.
	// These are OAuth tokens requiring Bearer auth.
	CredentialSourceGCM

	// CredentialSourceReplay indicates dummy credentials
	// for replay mode.
	CredentialSourceReplay
)

// Credential retrieves full authentication credentials (username and password)
// for the given forge URL. This is useful for forges that may need
// the username for API operations or user identification.
//
// In update mode, it tries environment variables first,
// then falls back to GCM.
// In replay mode, it returns dummy credentials.
//
// Returns the credential source so callers can select
// the appropriate auth type (Bearer for API tokens or OAuth).
func Credential(
	t *testing.T,
	forgeURL, userEnvVar, passEnvVar string,
) (username, password string, source CredentialSource) {
	if !Update() {
		return "user@example.com", "token", CredentialSourceReplay
	}

	// Try environment variables first for explicit override.
	user := os.Getenv(userEnvVar)
	pass := os.Getenv(passEnvVar)
	if user != "" && pass != "" {
		t.Logf("Using %s/%s from environment",
			userEnvVar, passEnvVar)
		return user, pass, CredentialSourceEnv
	}

	// Try GCM.
	cred, err := forge.LoadGCMCredential(t.Context(), forgeURL)
	if err == nil {
		t.Logf(
			"Using credentials from git-credential-manager for %s",
			forgeURL,
		)
		return cred.Username, cred.Password, CredentialSourceGCM
	}

	t.Fatalf(
		"No credentials available for %s: "+
			"set %s/%s or configure git-credential-manager",
		forgeURL, userEnvVar, passEnvVar,
	)
	return "", "", CredentialSourceReplay
}

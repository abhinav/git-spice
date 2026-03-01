package forgetest

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"go.abhg.dev/gs/internal/httptest"
	"gopkg.in/yaml.v3"
)

// TestConfig holds configuration for integration tests.
// This configuration is loaded from testconfig.yaml in update mode,
// and uses canonical placeholders in replay mode.
type TestConfig struct {
	GitHub    ForgeConfig `yaml:"github"`
	GitLab    ForgeConfig `yaml:"gitlab"`
	Bitbucket ForgeConfig `yaml:"bitbucket"`
}

// ForgeConfig holds per-forge test configuration.
type ForgeConfig struct {
	// Owner is the repository owner (GitHub/GitLab) or workspace (Bitbucket).
	Owner string `yaml:"owner"`

	// Repo is the repository name.
	Repo string `yaml:"repo"`

	// Reviewer is a username that can be added as a reviewer to changes.
	Reviewer string `yaml:"reviewer"`

	// Assignee is a username that can be assigned to changes.
	Assignee string `yaml:"assignee"`
}

type configState struct {
	config *TestConfig
	loadE  error
}

var (
	_configOnce  sync.Once
	_configState configState
)

// Config returns the test configuration.
// In replay mode, it returns canonical placeholders.
// In update mode, it loads from testconfig.yaml.
func Config(t *testing.T) *TestConfig {
	// In replay mode, always use canonical config.
	// This ensures requests match the sanitized fixtures.
	if !Update() {
		return canonicalConfig()
	}

	// In update mode, load from testconfig.yaml.
	_configOnce.Do(func() {
		_configState.config, _configState.loadE = loadConfig()
	})

	if _configState.loadE != nil {
		t.Fatalf("Failed to load test config: %v", _configState.loadE)
	}

	return _configState.config
}

// canonicalConfig returns the canonical placeholders used in VCR fixtures.
func canonicalConfig() *TestConfig {
	return &TestConfig{
		GitHub:    CanonicalGitHubConfig(),
		GitLab:    CanonicalGitLabConfig(),
		Bitbucket: CanonicalBitbucketConfig(),
	}
}

// CanonicalGitHubConfig returns canonical placeholders for GitHub fixtures.
func CanonicalGitHubConfig() ForgeConfig {
	return ForgeConfig{
		Owner:    CanonicalOwner,
		Repo:     CanonicalRepo,
		Reviewer: "test-owner-robot",
		Assignee: "test-owner-robot",
	}
}

// CanonicalGitLabConfig returns canonical placeholders for GitLab fixtures.
// Note: Uses the same value for Reviewer and Assignee because users often use
// the same account for both in test configurations. This prevents sanitization
// conflicts when both have the same value.
func CanonicalGitLabConfig() ForgeConfig {
	return ForgeConfig{
		Owner:    CanonicalOwner,
		Repo:     CanonicalRepo,
		Reviewer: "test-reviewer",
		Assignee: "test-reviewer", // Same as reviewer to handle value collisions
	}
}

// CanonicalBitbucketConfig returns canonical placeholders for Bitbucket fixtures.
// Note: Uses CanonicalOwner for both Owner and Repo because Bitbucket workspaces
// often have the same name as their primary repository (e.g., workspace "foo" with
// repo "foo"). This prevents sanitization conflicts when both have the same value.
func CanonicalBitbucketConfig() ForgeConfig {
	return ForgeConfig{
		Owner:    CanonicalOwner,
		Repo:     CanonicalOwner, // Same as owner to handle workspace/repo name collisions
		Reviewer: "Test Reviewer",
		Assignee: "",
	}
}

// loadConfig loads the test configuration from testconfig.yaml.
func loadConfig() (*TestConfig, error) {
	configPath := configFilePath()

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config TestConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// configFilePath returns the path to testconfig.yaml.
func configFilePath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	return filepath.Join(dir, "testconfig.yaml")
}

// ConfigSanitizers returns sanitizers for the given forge configuration.
// These replace actual values with canonical placeholders in VCR fixtures.
func ConfigSanitizers(cfg ForgeConfig, canonical ForgeConfig) []Sanitizer {
	var sanitizers []Sanitizer

	addSanitizer := func(actual, canonical string) {
		if actual != "" && actual != canonical {
			sanitizers = append(sanitizers, Sanitizer{
				Replace: actual,
				With:    canonical,
			})
		}
	}

	addSanitizer(cfg.Owner, canonical.Owner)
	addSanitizer(cfg.Repo, canonical.Repo)
	addSanitizer(cfg.Reviewer, canonical.Reviewer)
	addSanitizer(cfg.Assignee, canonical.Assignee)

	return sanitizers
}

// Sanitizer is re-exported from httptest for convenience.
type Sanitizer = httptest.Sanitizer

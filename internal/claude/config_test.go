package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	t.Run("DefaultConfig", func(t *testing.T) {
		cfg := DefaultConfig()

		assert.Equal(t, 4000, cfg.MaxLines)
		assert.NotEmpty(t, cfg.IgnorePatterns)
		assert.Contains(t, cfg.IgnorePatterns, "*.lock")
		assert.Contains(t, cfg.IgnorePatterns, "vendor/*")
		assert.NotEmpty(t, cfg.Prompts.Review)
		assert.NotEmpty(t, cfg.Prompts.Summary)
		assert.NotEmpty(t, cfg.Prompts.Commit)
		assert.NotEmpty(t, cfg.Prompts.StackReview)

		// Check default models.
		assert.Equal(t, ModelSonnet, cfg.Models.Review)
		assert.Equal(t, ModelHaiku, cfg.Models.Summary)
		assert.Equal(t, ModelHaiku, cfg.Models.Commit)
	})

	t.Run("LoadFromFile", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "claude.yaml")

		configContent := `
maxLines: 2000
ignorePatterns:
  - "*.test"
  - "custom/*"
prompts:
  review: "Custom review prompt"
  summary: "Custom summary prompt"
  commit: "Custom commit prompt"
  stackReview: "Custom stack review prompt"
refineOptions:
  - label: "Custom option"
    prompt: "Custom instruction"
`
		err := os.WriteFile(configPath, []byte(configContent), 0o644)
		require.NoError(t, err)

		cfg, err := LoadConfig(configPath)
		require.NoError(t, err)

		assert.Equal(t, 2000, cfg.MaxLines)
		assert.Equal(t, []string{"*.test", "custom/*"}, cfg.IgnorePatterns)
		assert.Equal(t, "Custom review prompt", cfg.Prompts.Review)
		assert.Equal(t, "Custom summary prompt", cfg.Prompts.Summary)
		assert.Equal(t, "Custom commit prompt", cfg.Prompts.Commit)
		assert.Equal(t, "Custom stack review prompt", cfg.Prompts.StackReview)
		require.Len(t, cfg.RefineOptions, 1)
		assert.Equal(t, "Custom option", cfg.RefineOptions[0].Label)
	})

	t.Run("LoadNonExistent", func(t *testing.T) {
		cfg, err := LoadConfig("/nonexistent/path/claude.yaml")
		require.NoError(t, err)

		// Should return default config.
		assert.Equal(t, 4000, cfg.MaxLines)
	})

	t.Run("PartialConfig", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "claude.yaml")

		// Only override maxLines, everything else should be default.
		configContent := `maxLines: 8000`
		err := os.WriteFile(configPath, []byte(configContent), 0o644)
		require.NoError(t, err)

		cfg, err := LoadConfig(configPath)
		require.NoError(t, err)

		assert.Equal(t, 8000, cfg.MaxLines)
		// Other fields should have defaults.
		assert.NotEmpty(t, cfg.IgnorePatterns)
		assert.NotEmpty(t, cfg.Prompts.Review)
	})

	t.Run("CustomModels", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "claude.yaml")

		configContent := `
models:
  review: "custom-review-model"
  summary: "custom-summary-model"
  commit: "custom-commit-model"
`
		err := os.WriteFile(configPath, []byte(configContent), 0o644)
		require.NoError(t, err)

		cfg, err := LoadConfig(configPath)
		require.NoError(t, err)

		assert.Equal(t, "custom-review-model", cfg.Models.Review)
		assert.Equal(t, "custom-summary-model", cfg.Models.Summary)
		assert.Equal(t, "custom-commit-model", cfg.Models.Commit)
	})

	t.Run("InvalidYAML", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "claude.yaml")

		configContent := `invalid: yaml: content:`
		err := os.WriteFile(configPath, []byte(configContent), 0o644)
		require.NoError(t, err)

		_, err = LoadConfig(configPath)
		assert.Error(t, err)
	})
}

func TestConfig_Validate(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		cfg := DefaultConfig()
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("ZeroMaxLines", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MaxLines = 0
		err := cfg.Validate()
		assert.Error(t, err)
	})

	t.Run("NegativeMaxLines", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MaxLines = -1
		err := cfg.Validate()
		assert.Error(t, err)
	})
}

func TestRefineOption(t *testing.T) {
	t.Run("DefaultRefineOptions", func(t *testing.T) {
		cfg := DefaultConfig()
		require.NotEmpty(t, cfg.RefineOptions)

		// Check for expected default options.
		labels := make([]string, 0, len(cfg.RefineOptions))
		for _, opt := range cfg.RefineOptions {
			labels = append(labels, opt.Label)
		}
		assert.Contains(t, labels, "Make it shorter")
		assert.Contains(t, labels, "Conventional commits")
	})
}

func TestConfigPath(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		path := DefaultConfigPath()
		assert.Contains(t, path, "git-spice")
		assert.Contains(t, path, "claude.yaml")
	})

	t.Run("WithXDGConfigHome", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tempDir)

		path := DefaultConfigPath()
		assert.True(t, strings.HasPrefix(path, tempDir))
		assert.Contains(t, path, "git-spice")
		assert.Contains(t, path, "claude.yaml")
	})
}

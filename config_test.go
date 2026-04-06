package main

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/cli/experiment"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/sigstack"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice"
)

// List of Git configuration sections besides "spice."
// that we read is hard-coded in spice/config.go.
// This tests that all sections that we need are covered.
func TestGitSectionsRequested(t *testing.T) {
	app, err := kong.New(
		new(mainCmd),
		kong.Vars{"defaultPrompt": "false"},
	)
	require.NoError(t, err)

	nodes := []*kong.Node{app.Model.Node}
	sections := make(map[string]struct{})
	for len(nodes) > 0 {
		node := nodes[0]
		nodes = append(nodes[1:], node.Children...)

		for _, flag := range node.Flags {
			key := flag.Tag.Get("config")
			gitKey, ok := strings.CutPrefix(key, "@")
			if !ok {
				continue
			}

			section, _, _ := git.ConfigKey(gitKey).Split()
			sections[section] = struct{}{}
		}
	}

	want := slices.Sorted(maps.Keys(sections))
	assert.ElementsMatch(t, want, spice.GitSections)
}

func TestMainSecretBackendConfig(t *testing.T) {
	tests := []struct {
		name   string
		env    string
		config string
		want   string
	}{
		{
			name: "Default",
			want: "auto",
		},
		{
			name: "Environment",
			env:  "file",
			want: "file",
		},
		{
			name: "Config",
			config: joinLines(
				`[spice "secret"]`,
				`  backend = keyring`,
			),
			want: "keyring",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("GIT_SPICE_SECRET_BACKEND", tt.env)
			}

			spicecfg := loadTestSpiceConfig(t, tt.config)

			var cmd mainCmd
			logger := silogtest.New(t)
			var (
				forges   forge.Registry
				sigStack sigstack.Stack
			)
			parser, err := kong.New(
				&cmd,
				kong.Resolvers(spicecfg),
				kong.Bind(logger, &forges, &sigStack),
				kong.BindTo(t.Context(), (*context.Context)(nil)),
				kong.BindTo(spicecfg, (*experiment.Enabler)(nil)),
				kong.Vars{"defaultPrompt": "false"},
			)
			require.NoError(t, err)

			_, err = parser.Parse([]string{"version"})
			require.NoError(t, err)
			assert.Equal(t, tt.want, cmd.Globals.SecretBackend.String())
		})
	}
}

func TestMainSecretBackendConfig_errors(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		config  string
		wantErr []string
	}{
		{
			name: "InvalidConfig",
			config: joinLines(
				`[spice "secret"]`,
				`  backend = invalid`,
			),
			wantErr: []string{
				`--secret-backend: invalid value "invalid": expected auto, file, or keyring`,
			},
		},
		{
			name: "InvalidEnvironmentOverridesConfig",
			env:  "invalid",
			config: joinLines(
				`[spice "secret"]`,
				`  backend = keyring`,
			),
			wantErr: []string{
				`--secret-backend: invalid value "invalid": expected auto, file, or keyring`,
				`GIT_SPICE_SECRET_BACKEND="invalid"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("GIT_SPICE_SECRET_BACKEND", tt.env)
			}

			spicecfg := loadTestSpiceConfig(t, tt.config)

			var cmd mainCmd
			logger := silogtest.New(t)
			var (
				forges   forge.Registry
				sigStack sigstack.Stack
			)
			parser, err := kong.New(
				&cmd,
				kong.Resolvers(spicecfg),
				kong.Bind(logger, &forges, &sigStack),
				kong.BindTo(t.Context(), (*context.Context)(nil)),
				kong.BindTo(spicecfg, (*experiment.Enabler)(nil)),
				kong.Vars{"defaultPrompt": "false"},
			)
			require.NoError(t, err)

			_, err = parser.Parse([]string{"version"})
			require.Error(t, err)
			for _, msg := range tt.wantErr {
				assert.ErrorContains(t, err, msg)
			}
		})
	}
}

func loadTestSpiceConfig(t *testing.T, cfg string) *spice.Config {
	t.Helper()

	home := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(home, ".gitconfig"),
		[]byte(cfg),
		0o600,
	))

	gitCfg := git.NewConfig(git.ConfigOptions{
		Log: silogtest.New(t),
		Dir: home,
		Env: []string{
			"HOME=" + home,
			"USER=testuser",
			"GIT_CONFIG_NOSYSTEM=1",
		},
	})

	spicecfg, err := spice.LoadConfig(t.Context(), gitCfg, spice.ConfigOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	return spicecfg
}

func joinLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/cli/experiment"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/sigstack"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/testing/stub"
)

func TestMainCmd_secretBackendBinding(t *testing.T) {
	getSelectedStash := func(t *testing.T, env string) (secret.Stash, secret.Stash, string) {
		t.Helper()

		var keyringStash secret.Stash = new(secret.MemoryStash)
		t.Cleanup(stub.Value(&_keyringStash, keyringStash))

		if env != "" {
			t.Setenv("GIT_SPICE_SECRET_BACKEND", env)
		}

		configDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", configDir)

		var cli struct {
			mainCmd
			Probe secretProbeCmd `cmd:""`
		}

		spicecfg := loadTestSpiceConfig(t, "")
		logger := silogtest.New(t)

		var (
			forges   forge.Registry
			sigStack sigstack.Stack
		)

		parser, err := kong.New(
			&cli,
			kong.Resolvers(spicecfg),
			kong.Bind(logger, &forges, &sigStack),
			kong.BindTo(t.Context(), (*context.Context)(nil)),
			kong.BindTo(spicecfg, (*experiment.Enabler)(nil)),
			kong.Vars{"defaultPrompt": "false"},
		)
		require.NoError(t, err)

		kctx, err := parser.Parse([]string{"probe"})
		require.NoError(t, err)
		require.NoError(t, kctx.Run())

		return cli.Probe.got, keyringStash, configDir
	}

	t.Run("auto", func(t *testing.T) {
		stash, keyringStash, configDir := getSelectedStash(t, "")

		fallback, ok := stash.(*secret.FallbackStash)
		require.True(t, ok)
		assert.Same(t, keyringStash, fallback.Primary)

		insecure, ok := fallback.Secondary.(*secret.InsecureStash)
		require.True(t, ok)
		assert.Equal(t,
			filepath.Join(configDir, "git-spice", "secrets.json"),
			insecure.Path,
		)
	})

	t.Run("file", func(t *testing.T) {
		stash, keyringStash, configDir := getSelectedStash(t, "file")

		assert.NotSame(t, keyringStash, stash)
		_, ok := stash.(*secret.FallbackStash)
		assert.False(t, ok)
		require.NoError(t, stash.SaveSecret("service", "key", "secret"))
		_, err := os.Stat(filepath.Join(configDir, "git-spice", "secrets.json"))
		require.NoError(t, err)
	})

	t.Run("keyring", func(t *testing.T) {
		stash, keyringStash, _ := getSelectedStash(t, "keyring")

		assert.Same(t, keyringStash, stash)
		_, ok := stash.(*secret.FallbackStash)
		assert.False(t, ok)
	})
}

type secretProbeCmd struct {
	got secret.Stash
}

func (cmd *secretProbeCmd) Run(stash secret.Stash) error {
	cmd.got = stash
	return nil
}

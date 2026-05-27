package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog"
)

func TestSeriesFlag_Set(t *testing.T) {
	var series seriesFlag

	require.NoError(t, series.Set("noble,plucky"))
	require.NoError(t, series.Set("questing"))
	require.NoError(t, series.Set(" resolute, "))

	assert.Equal(t,
		seriesFlag{"noble", "plucky", "questing", "resolute"},
		series)
}

func TestNewPackagePlan_defaults(t *testing.T) {
	plan, err := newPackagePlan(publishRequest{
		Version:     "v0.28.0",
		PPARevision: 1,
		DputTarget:  _defaultDputTarget,
	})

	require.NoError(t, err)
	assert.Equal(t, "v0.28.0", plan.Version)
	assert.Equal(t, "0.28.0", plan.UpstreamVersion)
	assert.Equal(t, "0.28.0-1~ppa1", plan.BaseDebianVersion)
	assert.True(t, plan.SourceModTime.IsZero())
	assert.Equal(t, "v0.28.0", plan.Ref)
	assert.Equal(t, []string{"noble"}, plan.Series)
	assert.Nil(t, plan.Sign)
	assert.False(t, plan.Dput)
	assert.Equal(t, _defaultDputTarget, plan.DputTarget)
}

func TestNewPackagePlan_customValues(t *testing.T) {
	plan, err := newPackagePlan(publishRequest{
		Version:         "v0.28.0",
		Ref:             "release-branch",
		SourceDateEpoch: 1_779_770_939,
		Series:          seriesFlag{"noble", "plucky"},
		PPARevision:     2,
		Dput:            true,
		DputTarget:      "ppa:test/git-spice",
	})

	require.NoError(t, err)
	assert.Equal(t, "release-branch", plan.Ref)
	assert.Equal(t, []string{"noble", "plucky"}, plan.Series)
	assert.Equal(t, "0.28.0-1~ppa2", plan.BaseDebianVersion)
	assert.Equal(t,
		time.Unix(1_779_770_939, 0).UTC(),
		plan.SourceModTime)
	assert.Nil(t, plan.Sign)
	assert.True(t, plan.Dput)
	assert.Equal(t, "ppa:test/git-spice", plan.DputTarget)
}

func TestNewPackagePlan_invalid(t *testing.T) {
	_, err := newPackagePlan(publishRequest{
		Version:     "not-a-version",
		PPARevision: 0,
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "version must be a valid semantic version")
	assert.ErrorContains(t, err, "PPA revision must be positive")
	assert.ErrorContains(t, err, "-dput-target is required")
}

func TestNewPackagePlan_invalidSourceDateEpoch(t *testing.T) {
	_, err := newPackagePlan(publishRequest{
		Version:         "v0.28.0",
		SourceDateEpoch: -1,
		PPARevision:     1,
		DputTarget:      _defaultDputTarget,
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "-source-date-epoch must be positive: -1")
}

func TestSignConfigFromEnv(t *testing.T) {
	t.Run("Disabled", func(t *testing.T) {
		sign, err := signConfigFromEnv(false)
		require.NoError(t, err)
		assert.Nil(t, sign)
	})

	t.Run("Valid", func(t *testing.T) {
		t.Setenv(_launchpadGPGKeyIDEnv, "ABC123")
		t.Setenv(_launchpadGPGPassphraseEnv, "secret")

		sign, err := signConfigFromEnv(true)
		require.NoError(t, err)
		require.NotNil(t, sign)
		assert.Equal(t, "ABC123", sign.KeyID)
		assert.Equal(t, "secret", sign.Passphrase)
	})

	t.Run("MissingKey", func(t *testing.T) {
		t.Setenv(_launchpadGPGPassphraseEnv, "secret")

		_, err := signConfigFromEnv(true)
		require.Error(t, err)
		assert.ErrorContains(t, err, _launchpadGPGKeyIDEnv)
	})

	t.Run("MissingPassphrase", func(t *testing.T) {
		t.Setenv(_launchpadGPGKeyIDEnv, "ABC123")

		_, err := signConfigFromEnv(true)
		require.Error(t, err)
		assert.ErrorContains(t, err, _launchpadGPGPassphraseEnv)
	})
}

func TestRenderDputCommand(t *testing.T) {
	assert.Equal(t,
		"dput ppa:abhg/git-spice dist/debian/noble/git-spice_0.28.0-1~ppa1~ubuntu24.04.1_source.changes",
		renderDputCommand(
			"ppa:abhg/git-spice",
			"dist/debian/noble/git-spice_0.28.0-1~ppa1~ubuntu24.04.1_source.changes",
		))
}

func TestWriteOrigTar_usesSourceModificationTime(t *testing.T) {
	sourceDir := t.TempDir()
	require.NoError(t,
		os.WriteFile(filepath.Join(sourceDir, "README.md"), []byte("hello\n"), 0o644))

	dest := filepath.Join(t.TempDir(), "git-spice_0.29.0.orig.tar.gz")
	wantTime := time.Unix(1_700_000_000, 0).UTC()

	require.NoError(t, writeOrigTar(silog.Nop(), sourceDir, dest, wantTime))

	f, err := os.Open(dest)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, f.Close())
	})

	gz, err := gzip.NewReader(f)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, gz.Close())
	})
	assert.Equal(t, wantTime.Unix(), gz.ModTime.Unix())

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			t.Fatal("README.md not found in orig tarball")
		}
		require.NoError(t, err)

		if header.Name != "./README.md" {
			continue
		}

		assert.Equal(t, wantTime.Unix(), header.ModTime.Unix())
		assert.Equal(t, wantTime.Unix(), header.AccessTime.Unix())
		assert.Equal(t, wantTime.Unix(), header.ChangeTime.Unix())
		return
	}
}

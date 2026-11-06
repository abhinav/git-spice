//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestUploadSourcePackage_retriesTwice(t *testing.T) {
	binDir := t.TempDir()
	attemptsFile := filepath.Join(t.TempDir(), "attempts")
	writeRetryExecutable(t, filepath.Join(binDir, "dput"), `#!/bin/sh
set -eu
attempts=0
if [ -f "$DPUT_ATTEMPTS_FILE" ]; then
  attempts=$(cat "$DPUT_ATTEMPTS_FILE")
fi
attempts=$((attempts + 1))
printf '%s' "$attempts" > "$DPUT_ATTEMPTS_FILE"
if [ "$attempts" -lt 3 ]; then
  exit 1
fi
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DPUT_ATTEMPTS_FILE", attemptsFile)

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		err := uploadSourcePackage(
			t.Context(),
			silogtest.New(t),
			"ppa:test/git-spice",
			"package_source.changes",
		)
		require.NoError(t, err)
		assert.Equal(t, 45*time.Second, time.Since(start))
	})

	attempts, err := os.ReadFile(attemptsFile)
	require.NoError(t, err)
	assert.Equal(t, "3", string(attempts))
}

func TestUploadSourcePackage_stopsAfterThirdAttempt(t *testing.T) {
	binDir := t.TempDir()
	attemptsFile := filepath.Join(t.TempDir(), "attempts")
	writeRetryExecutable(t, filepath.Join(binDir, "dput"), `#!/bin/sh
set -eu
attempts=0
if [ -f "$DPUT_ATTEMPTS_FILE" ]; then
  attempts=$(cat "$DPUT_ATTEMPTS_FILE")
fi
attempts=$((attempts + 1))
printf '%s' "$attempts" > "$DPUT_ATTEMPTS_FILE"
exit 1
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DPUT_ATTEMPTS_FILE", attemptsFile)

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		err := uploadSourcePackage(
			t.Context(),
			silogtest.New(t),
			"ppa:test/git-spice",
			"package_source.changes",
		)
		require.Error(t, err)
		assert.Equal(t, 45*time.Second, time.Since(start))
	})

	attempts, err := os.ReadFile(attemptsFile)
	require.NoError(t, err)
	assert.Equal(t, "3", string(attempts))
}

func writeRetryExecutable(t *testing.T, path string, body string) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte(body), 0o755))
}

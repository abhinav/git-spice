package forge

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCredentialOutput(t *testing.T) {
	tests := []struct {
		name         string
		output       string
		wantUsername string
		wantPassword string
		wantErr      bool
	}{
		{
			name: "ValidCredentials",
			output: `protocol=https
host=bitbucket.org
username=user@example.com
password=oauth-token-here
`,
			wantUsername: "user@example.com",
			wantPassword: "oauth-token-here",
		},
		{
			name: "PasswordOnly",
			output: `protocol=https
host=bitbucket.org
password=token-only
`,
			wantUsername: "",
			wantPassword: "token-only",
		},
		{
			name: "ExtraFields",
			output: `protocol=https
host=bitbucket.org
username=user
password=secret
path=/repo
`,
			wantUsername: "user",
			wantPassword: "secret",
		},
		{
			name:    "EmptyOutput",
			output:  "",
			wantErr: true,
		},
		{
			name: "NoPassword",
			output: `protocol=https
host=bitbucket.org
username=user
`,
			wantErr: true,
		},
		{
			name: "MalformedLines",
			output: `protocol=https
invalid-line-without-equals
password=secret
`,
			wantPassword: "secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cred, err := parseCredentialOutput([]byte(tt.output))

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantUsername, cred.Username)
			assert.Equal(t, tt.wantPassword, cred.Password)
		})
	}
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantOut string
	}{
		{
			name:    "HTTPS",
			rawURL:  "https://bitbucket.org",
			wantOut: "bitbucket.org",
		},
		{
			name:    "HTTPSWithPath",
			rawURL:  "https://bitbucket.org/workspace/repo",
			wantOut: "bitbucket.org",
		},
		{
			name:    "HTTPSWithPort",
			rawURL:  "https://bitbucket.example.com:443",
			wantOut: "bitbucket.example.com:443",
		},
		{
			name:    "HTTP",
			rawURL:  "http://bitbucket.local",
			wantOut: "bitbucket.local",
		},
		{
			name:    "NoProtocol",
			rawURL:  "bitbucket.org",
			wantOut: "bitbucket.org",
		},
		{
			name:    "NoProtocolWithPath",
			rawURL:  "bitbucket.org/workspace",
			wantOut: "bitbucket.org/workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantOut, extractHost(tt.rawURL))
		})
	}
}

func TestLoadGCMCredential_disablesTerminalPrompt(t *testing.T) {
	// Regression test for issue #1120:
	// https://github.com/abhinav/git-spice/issues/1120
	dir := t.TempDir()
	gitPath := filepath.Join(dir, "git")
	if runtime.GOOS == "windows" {
		gitPath += ".exe"
	}

	testExe, err := os.Executable()
	require.NoError(t, err)
	createFakeGitExecutable(t, testExe, gitPath)

	t.Setenv(
		"PATH",
		dir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	t.Setenv("GS_GCM_TEST_FAKE_GIT_MODE", "gcm-fill")

	cred, err := LoadGCMCredential(
		t.Context(),
		"https://github.com/git-spice/git-spice",
	)
	require.NoError(t, err)
	assert.Equal(t, "test-user", cred.Username)
	assert.Equal(t, "test-token", cred.Password)
}

func createFakeGitExecutable(
	t *testing.T,
	testExe string,
	gitPath string,
) {
	t.Helper()

	if err := os.Symlink(testExe, gitPath); err == nil {
		return
	}

	src, err := os.Open(testExe)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, src.Close())
	}()

	dst, err := os.Create(gitPath)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, dst.Close())
	}()

	_, err = io.Copy(dst, src)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(gitPath, 0o755))
}

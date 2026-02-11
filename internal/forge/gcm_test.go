package forge

import (
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
			wantOut: "bitbucket.org",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantOut, extractHost(tt.rawURL))
		})
	}
}

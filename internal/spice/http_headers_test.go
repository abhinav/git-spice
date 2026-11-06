package spice_test

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/httpclient"
	"go.abhg.dev/gs/internal/spice"
)

func TestConfig_HTTPHeaderRules(t *testing.T) {
	t.Setenv("GS_HEADER_TOKEN", "secret-$LITERAL")
	t.Setenv("GS_HEADER_EMPTY", "")
	t.Setenv("GS_HEADER_UNSET", "")
	require.NoError(t, os.Unsetenv("GS_HEADER_UNSET"))

	configFile := filepath.Join(t.TempDir(), "gitconfig")
	entries := []git.ConfigEntry{
		{
			Key:   "spice.http.https://EXAMPLE.com:443/.header",
			Value: "X-Literal: $GS_HEADER_TOKEN",
		},
		{
			Key:   "spice.http.https://EXAMPLE.com:443/.header",
			Value: "X-Commas: a,b:c",
		},
		{
			Key:   "spice.http.https://EXAMPLE.com:443/.headerFromEnv",
			Value: "Cf-Access-Token: $GS_HEADER_TOKEN",
		},
		{
			Key:   "spice.http.https://EXAMPLE.com:443/.headerFromEnv",
			Value: "X-Combined: Bearer ${GS_HEADER_TOKEN}",
		},
		{
			Key:   "spice.http.https://EXAMPLE.com:443/.headerFromEnv",
			Value: "X-Empty: $GS_HEADER_EMPTY",
		},
		{
			Key:   "spice.http.https://EXAMPLE.com:443/.headerFromEnv",
			Value: "X-Missing: $GS_HEADER_UNSET",
		},
		{
			Key:   "spice.http.https://EXAMPLE.com:443/.headerFromEnv",
			Value: "X-Malformed: ${",
		},
	}
	for _, entry := range entries {
		cmd := exec.CommandContext(
			t.Context(), "git", "config", "--file", configFile,
			"--add", string(entry.Key), entry.Value,
		)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}

	gitConfig := git.NewConfig(git.ConfigOptions{
		Dir: filepath.Dir(configFile),
		Env: []string{
			"GIT_CONFIG_GLOBAL=" + configFile,
			"GIT_CONFIG_NOSYSTEM=1",
		},
	})
	config, err := spice.LoadConfig(
		t.Context(), gitConfig, spice.ConfigOptions{},
	)
	require.NoError(t, err)

	rules, err := config.HTTPHeaderRules()
	require.NoError(t, err)
	capture := &configHeaderCapture{}
	transport, err := httpclient.NewHeaderTransport(capture, rules)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodGet, "https://example.com/api", nil,
	)
	require.NoError(t, err)
	_, err = transport.RoundTrip(req)
	require.NoError(t, err)

	assert.Equal(t, http.Header{
		"X-Literal":       []string{"$GS_HEADER_TOKEN"},
		"X-Commas":        []string{"a,b:c"},
		"Cf-Access-Token": []string{"secret-$LITERAL"},
		"X-Combined":      []string{"Bearer secret-$LITERAL"},
		"X-Empty":         []string{""},
		"X-Missing":       []string{""},
		"X-Malformed":     []string{""},
	}, capture.header)
}

func TestConfig_HTTPHeaderRules_invalidConfig(t *testing.T) {
	tests := []struct {
		name string
		give []git.ConfigEntry
		want string
	}{
		{
			name: "Scope",
			give: []git.ConfigEntry{
				{
					Key:   "spice.http.http://example.com.header",
					Value: "X-Test: secret",
				},
			},
			want: "HTTPS origin",
		},
		{
			name: "MissingColon",
			give: []git.ConfigEntry{
				{
					Key:   "spice.http.https://example.com.header",
					Value: "secret",
				},
			},
			want: "header name followed by a colon",
		},
		{
			name: "Duplicate",
			give: []git.ConfigEntry{
				{
					Key:   "spice.http.https://EXAMPLE.com:443.header",
					Value: "X-Test: secret-one",
				},
				{
					Key:   "spice.http.https://example.com/.headerFromEnv",
					Value: "x-test: secret-two",
				},
			},
			want: "duplicate HTTP header",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configFile := filepath.Join(t.TempDir(), "gitconfig")
			for _, entry := range tt.give {
				cmd := exec.CommandContext(
					t.Context(), "git", "config", "--file", configFile,
					"--add", string(entry.Key), entry.Value,
				)
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
			}

			gitConfig := git.NewConfig(git.ConfigOptions{
				Dir: filepath.Dir(configFile),
				Env: []string{
					"GIT_CONFIG_GLOBAL=" + configFile,
					"GIT_CONFIG_NOSYSTEM=1",
				},
			})
			config, err := spice.LoadConfig(
				t.Context(), gitConfig, spice.ConfigOptions{},
			)
			require.NoError(t, err)

			rules, err := config.HTTPHeaderRules()
			if err == nil {
				_, err = httpclient.NewHeaderTransport(nil, rules)
			}

			require.ErrorContains(t, err, tt.want)
			assert.NotContains(t, err.Error(), "secret")
		})
	}
}

type configHeaderCapture struct {
	header http.Header
}

func (c *configHeaderCapture) RoundTrip(req *http.Request) (*http.Response, error) {
	c.header = req.Header.Clone()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       http.NoBody,
	}, nil
}

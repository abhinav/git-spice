package state

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoInfoUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		give string
		want repoInfo
	}{
		{
			name: "legacy string",
			give: `{"trunk":"main","remote":"origin"}`,
			want: repoInfo{
				Trunk:  "main",
				Remote: "origin",
			},
		},
		{
			name: "remotes object",
			give: `{"trunk":"main","remotes":{"upstream":"upstream","push":"origin"}}`,
			want: repoInfo{
				Trunk: "main",
				Remotes: &remoteInfo{
					Upstream: "upstream",
					Push:     "origin",
				},
			},
		},
		{
			name: "previous remote object",
			give: `{"trunk":"main","remote":{"upstream":"upstream","push":"origin"}}`,
			want: repoInfo{
				Trunk: "main",
				Remotes: &remoteInfo{
					Upstream: "upstream",
					Push:     "origin",
				},
			},
		},
		{
			name: "empty remote string",
			give: `{"trunk":"main","remote":""}`,
			want: repoInfo{Trunk: "main"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got repoInfo
			require.NoError(t, json.Unmarshal([]byte(tt.give), &got))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRemoteForkMode(t *testing.T) {
	tests := []struct {
		name string
		give Remote
		want bool
	}{
		{name: "empty"},
		{
			name: "same",
			give: Remote{
				Upstream: "origin",
				Push:     "origin",
			},
		},
		{
			name: "different",
			give: Remote{
				Upstream: "upstream",
				Push:     "origin",
			},
			want: true,
		},
		{
			name: "missing push",
			give: Remote{Upstream: "upstream"},
		},
		{
			name: "missing upstream",
			give: Remote{Push: "origin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.ForkMode())
		})
	}
}

func TestRepoInfoValidate(t *testing.T) {
	tests := []struct {
		name    string
		give    repoInfo
		wantErr string
	}{
		{
			name:    "empty",
			give:    repoInfo{},
			wantErr: "trunk branch name is empty",
		},
		{
			name: "valid",
			give: repoInfo{Trunk: "main"},
		},
		{
			name: "valid with remote",
			give: repoInfo{
				Trunk: "main",
				Remotes: &remoteInfo{
					Upstream: "origin",
					Push:     "origin",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.give.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

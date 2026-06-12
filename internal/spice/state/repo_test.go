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
		{
			name: "integration full",
			give: `{
				"trunk": "main",
				"integration": {
					"name": "preview",
					"upstreamBranch": "preview",
					"lastPushedHash": "abc123",
					"tips": [
						{"name": "feat-a", "hash": "def456"},
						{"name": "feat-b", "hash": "789abc"}
					]
				}
			}`,
			want: repoInfo{
				Trunk: "main",
				Integration: &integrationInfo{
					Name:           "preview",
					UpstreamBranch: "preview",
					LastPushedHash: "abc123",
					Tips: []integrationTipInfo{
						{Name: "feat-a", Hash: "def456"},
						{Name: "feat-b", Hash: "789abc"},
					},
				},
			},
		},
		{
			name: "integration minimal",
			give: `{
				"trunk": "main",
				"integration": {
					"name": "preview"
				}
			}`,
			want: repoInfo{
				Trunk: "main",
				Integration: &integrationInfo{
					Name: "preview",
				},
			},
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
		{
			name: "valid with integration",
			give: repoInfo{
				Trunk: "main",
				Integration: &integrationInfo{
					Name: "preview",
					Tips: []integrationTipInfo{
						{Name: "feat-a"},
					},
				},
			},
		},
		{
			name: "integration empty name",
			give: repoInfo{
				Trunk: "main",
				Integration: &integrationInfo{
					Name: "",
				},
			},
			wantErr: "integration branch name is empty",
		},
		{
			name: "integration name equals trunk",
			give: repoInfo{
				Trunk: "main",
				Integration: &integrationInfo{
					Name: "main",
				},
			},
			wantErr: "integration branch name must not equal trunk",
		},
		{
			name: "integration tip empty name",
			give: repoInfo{
				Trunk: "main",
				Integration: &integrationInfo{
					Name: "preview",
					Tips: []integrationTipInfo{
						{Name: ""},
					},
				},
			},
			wantErr: "integration tip name is empty",
		},
		{
			name: "integration tip equals trunk",
			give: repoInfo{
				Trunk: "main",
				Integration: &integrationInfo{
					Name: "preview",
					Tips: []integrationTipInfo{
						{Name: "main"},
					},
				},
			},
			wantErr: "integration tip must not equal trunk",
		},
		{
			name: "integration tip equals integration name",
			give: repoInfo{
				Trunk: "main",
				Integration: &integrationInfo{
					Name: "preview",
					Tips: []integrationTipInfo{
						{Name: "preview"},
					},
				},
			},
			wantErr: "integration tip must not equal integration branch name",
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

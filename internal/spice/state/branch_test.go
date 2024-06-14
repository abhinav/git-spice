package state

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchChangeStateUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		give string

		want    *branchChangeState
		wantErr string
	}{
		{
			name: "Valid",
			give: `{"github": {"number": 123}}`,
			want: &branchChangeState{
				Forge:  "github",
				Change: json.RawMessage(`{"number": 123}`),
			},
		},
		{
			name:    "NotAnObject",
			give:    `123`,
			wantErr: "unmarshal change state",
		},
		{
			name: "MultipleForges",
			give: `{
				"github": {"number": 123},
				"gitlab": {"number": 456}
			}`,
			wantErr: "expected 1 forge key, got 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got branchChangeState
			err := json.Unmarshal([]byte(tt.give), &got)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, &got)
		})
	}
}

func TestBranchStateUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		give string

		want    *branchState
		wantErr string
	}{
		{
			name: "Simple",
			give: `{
				"base": {"name": "main", "hash": "abc123"},
				"upstream": {"branch": "main"},
				"change": {"github": {"number": 123}}
			}`,
			want: &branchState{
				Base: branchStateBase{
					Name: "main",
					Hash: "abc123",
				},
				Upstream: &branchUpstreamState{
					Branch: "main",
				},
				Change: &branchChangeState{
					Forge:  "github",
					Change: json.RawMessage(`{"number": 123}`),
				},
			},
		},

		{
			name: "NoUpstream",
			give: `{
				"base": {"name": "main", "hash": "abc123"}
			}`,
			want: &branchState{
				Base: branchStateBase{
					Name: "main",
					Hash: "abc123",
				},
			},
		},

		{
			name: "UpgradeGitHub",
			give: `{
				"base": {"name": "main", "hash": "abc123"},
				"upstream": {"branch": "main"},
				"github": {"pr": 123}
			}`,
			want: &branchState{
				Base: branchStateBase{
					Name: "main",
					Hash: "abc123",
				},
				Upstream: &branchUpstreamState{
					Branch: "main",
				},
				Change: &branchChangeState{
					Forge:  "github",
					Change: json.RawMessage(`{"pr": 123}`),
				},
			},
		},

		{
			name: "UpgradeGitHubConflict/SameForge",
			give: `{
				"base": {"name": "main", "hash": "abc123"},
				"upstream": {"branch": "main"},
				"github": {"pr": 123},
				"change": {"github": {"number": 456}}
			}`,
			want: &branchState{
				Base: branchStateBase{
					Name: "main",
					Hash: "abc123",
				},
				Upstream: &branchUpstreamState{
					Branch: "main",
				},
				Change: &branchChangeState{
					Forge:  "github",
					Change: json.RawMessage(`{"number": 456}`),
				},
			},
		},

		{
			name: "UpgradeGitHubConflict/OtherForge",
			give: `{
				"base": {"name": "main", "hash": "abc123"},
				"upstream": {"branch": "main"},
				"github": {"pr": 123},
				"change": {"gitlab": {"number": 123}}
			}`,
			wantErr: "branch state has mixed forge metadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got branchState
			err := json.Unmarshal([]byte(tt.give), &got)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, &got)
		})
	}
}

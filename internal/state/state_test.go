package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoinfoValidate(t *testing.T) {
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
			give: repoInfo{Trunk: "main", Remote: "origin"},
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

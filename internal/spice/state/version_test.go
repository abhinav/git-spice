package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestLoadVersion(t *testing.T) {
	tests := []struct {
		name  string
		files storage.MapBackend
		want  Version
	}{
		{
			name: "Empty",
			want: VersionOne,
		},
		{
			name: "ExplicitV1",
			files: storage.MapBackend{
				"version": []byte("1"),
			},
			want: VersionOne,
		},
		{
			name: "ExplicitV2",
			files: storage.MapBackend{
				"version": []byte("2"),
			},
			want: VersionTwo,
		},
		{
			name: "FutureVersion",
			files: storage.MapBackend{
				"version": []byte("42"),
			},
			want: Version(42),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := storage.NewDB(tt.files)
			got, err := loadVersion(t.Context(), db)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckVersion(t *testing.T) {
	tests := []struct {
		name    string
		version Version
		err     bool
	}{
		{name: "VersionOne", version: VersionOne},
		{name: "VersionTwo", version: VersionTwo},
		{
			name:    "UnsupportedVersion",
			version: Version(500),
			err:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkVersion(tt.version)
			if tt.err {
				require.Error(t, err)
				assert.ErrorAs(t, err, new(*VersionMismatchError))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVersionMismatchError(t *testing.T) {
	err := &VersionMismatchError{
		Want: 42,
		Got:  43,
	}

	assert.Equal(t, "expected store version <= 42, got 43", err.Error())
}

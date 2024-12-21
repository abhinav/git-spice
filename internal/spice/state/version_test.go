package state

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/spice/state/storage"
	"go.uber.org/mock/gomock"
)

func TestLoadVersion(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  Version
	}{
		{
			name: "Empty",
			want: VersionOne,
		},
		{
			name: "ExplicitV1",
			files: map[string]string{
				"version": "1",
			},
			want: VersionOne,
		},
		{
			name: "FutureVersion",
			files: map[string]string{
				"version": "42",
			},
			want: Version(42),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := storage.NewMemBackend()
			mem.AddFiles(func(yield func(string, []byte) bool) {
				for name, body := range tt.files {
					if !yield(name, []byte(body)) {
						return
					}
				}
			})

			db := storage.NewDB(mem)
			got, err := loadVersion(context.Background(), db)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckVersion(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		err   bool
	}{
		{name: "ImplicitV1"},
		{
			name: "ExplicitV1",
			files: map[string]string{
				"version": "1",
			},
		},
		{
			name: "UnsupportedVersion",
			files: map[string]string{
				"version": "500",
			},
			err: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := storage.NewMemBackend()
			mem.AddFiles(func(yield func(string, []byte) bool) {
				for name, body := range tt.files {
					if !yield(name, []byte(body)) {
						return
					}
				}
			})

			db := storage.NewDB(mem)
			err := checkVersion(context.Background(), db)
			if tt.err {
				require.Error(t, err)
				assert.ErrorAs(t, err, new(*VersionMismatchError))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckVersion_loadError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := NewMockDB(ctrl)

	mockDB.EXPECT().
		Get(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(assert.AnError)

	err := checkVersion(context.Background(), mockDB)
	require.Error(t, err)
	assert.ErrorContains(t, err, "load store version:")
	assert.ErrorIs(t, err, assert.AnError)
}

func TestVersionMismatchError(t *testing.T) {
	err := &VersionMismatchError{
		Want: 42,
		Got:  43,
	}

	assert.Equal(t, "expected store version <= 42, got 43", err.Error())
}

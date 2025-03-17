package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushStatusFormat(t *testing.T) {
	tests := []struct {
		give        string
		want        pushStatusFormat
		wantStr     string
		wantEnabled bool
	}{
		{
			give:        "true",
			want:        pushStatusEnabled,
			wantStr:     "true",
			wantEnabled: true,
		},
		{
			give:        "yes",
			want:        pushStatusEnabled,
			wantStr:     "true",
			wantEnabled: true,
		},
		{
			give:        "false",
			want:        pushStatusDisabled,
			wantStr:     "false",
			wantEnabled: false,
		},
		{
			give:        "no",
			want:        pushStatusDisabled,
			wantStr:     "false",
			wantEnabled: false,
		},
		{
			give:        "aheadbehind",
			want:        pushStatusAheadBehind,
			wantStr:     "aheadBehind",
			wantEnabled: true,
		},
		{
			give:        "aheadBehind",
			want:        pushStatusAheadBehind,
			wantStr:     "aheadBehind",
			wantEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			t.Run("Unmarshal", func(t *testing.T) {
				var got pushStatusFormat
				err := got.UnmarshalText([]byte(tt.give))
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})

			t.Run("String", func(t *testing.T) {
				got := tt.want.String()
				assert.Equal(t, tt.wantStr, got)
			})

			t.Run("Enabled", func(t *testing.T) {
				got := tt.want.Enabled()
				assert.Equal(t, tt.wantEnabled, got)
			})
		})
	}

	t.Run("invalid", func(t *testing.T) {
		t.Run("Unmarshal", func(t *testing.T) {
			var got pushStatusFormat
			err := got.UnmarshalText([]byte("invalid"))
			require.Error(t, err)
		})

		t.Run("String", func(t *testing.T) {
			got := pushStatusFormat(999) // invalid value
			assert.Equal(t, "unknown", got.String())
		})

		t.Run("Enabled", func(t *testing.T) {
			got := pushStatusFormat(999) // invalid value
			assert.False(t, got.Enabled())
		})
	})
}

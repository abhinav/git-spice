package submit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkipRestackCheck_UnmarshalText(t *testing.T) {
	tests := []struct {
		name    string
		give    string
		want    SkipRestackCheck
		wantErr string
	}{
		{
			name: "Never",
			give: "never",
			want: SkipRestackCheckNever,
		},
		{
			name: "False",
			give: "false",
			want: SkipRestackCheckNever,
		},
		{
			name: "Trunk",
			give: "trunk",
			want: SkipRestackCheckTrunk,
		},
		{
			name: "Always",
			give: "always",
			want: SkipRestackCheckAlways,
		},
		{
			name: "True",
			give: "true",
			want: SkipRestackCheckAlways,
		},
		{
			name: "CaseInsensitive/Never",
			give: "NEVER",
			want: SkipRestackCheckNever,
		},
		{
			name: "CaseInsensitive/Trunk",
			give: "Trunk",
			want: SkipRestackCheckTrunk,
		},
		{
			name: "CaseInsensitive/Always",
			give: "ALWAYS",
			want: SkipRestackCheckAlways,
		},
		{
			name: "CaseInsensitive/True",
			give: "TRUE",
			want: SkipRestackCheckAlways,
		},
		{
			name:    "Invalid",
			give:    "garbage",
			wantErr: `invalid value "garbage": expected never, trunk, or always`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got SkipRestackCheck
			err := got.UnmarshalText([]byte(tt.give))

			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSkipRestackCheck_String(t *testing.T) {
	tests := []struct {
		name string
		give SkipRestackCheck
		want string
	}{
		{name: "Never", give: SkipRestackCheckNever, want: "never"},
		{name: "Trunk", give: SkipRestackCheckTrunk, want: "trunk"},
		{name: "Always", give: SkipRestackCheckAlways, want: "always"},
		{name: "Unknown", give: SkipRestackCheck(99), want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.String())
		})
	}
}

func TestShouldSkipRestackCheck(t *testing.T) {
	tests := []struct {
		name  string
		mode  SkipRestackCheck
		base  string
		trunk string
		want  bool
	}{
		{
			name: "Never",
			mode: SkipRestackCheckNever,
			base: "main", trunk: "main",
			want: false,
		},
		{
			name: "Always",
			mode: SkipRestackCheckAlways,
			base: "feature1", trunk: "main",
			want: true,
		},
		{
			name: "TrunkBasedOnTrunk",
			mode: SkipRestackCheckTrunk,
			base: "main", trunk: "main",
			want: true,
		},
		{
			name: "TrunkBasedOnNonTrunk",
			mode: SkipRestackCheckTrunk,
			base: "feature1", trunk: "main",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkipRestackCheck(
				tt.mode, tt.base, tt.trunk,
			)
			assert.Equal(t, tt.want, got)
		})
	}
}

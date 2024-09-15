package gittest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name string
		give string
		want Version
		str  string
	}{
		{
			name: "DarwinDefault",
			give: "git version 2.39.3 (Apple Git-128)",
			want: Version{2, 39, 3},
			str:  "2.39.3",
		},
		{
			name: "NoCustom",
			give: "git version 2.46.0",
			want: Version{2, 46, 0},
			str:  "2.46.0",
		},
		{
			name: "JustVersion",
			give: "2.39.3",
			want: Version{2, 39, 3},
			str:  "2.39.3",
		},
		{
			name: "MajorMinor",
			give: "2.39",
			want: Version{2, 39, 0},
			str:  "2.39.0",
		},
		{
			name: "MajorOnly",
			give: "2",
			want: Version{2, 0, 0},
			str:  "2.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVersion(tt.give)
			require.NoError(t, err)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.str, got.String())
		})
	}
}

func FuzzParseVersion(f *testing.F) {
	f.Add("git version 2.39.3 (Apple Git-128)")
	f.Add("git version 2.46.0")
	f.Add("2.39.3")
	f.Add("2.39")
	f.Add("2")
	f.Fuzz(func(t *testing.T, give string) {
		v, err := ParseVersion(give)
		if err != nil {
			return // ok
		}

		parsed, err := ParseVersion(v.String())
		require.NoError(t, err)

		assert.Equal(t, v, parsed)
	})
}

func TestParseVersionErrors(t *testing.T) {
	tests := []struct {
		name     string
		give     string
		wantErrs []string
	}{
		{
			name: "MajorNotInt",
			give: "git version a.39.3",
			wantErrs: []string{
				`bad major version "a"`,
			},
		},
		{
			name: "MinorNotInt",
			give: "git version 2.b.3",
			wantErrs: []string{
				`bad minor version "b"`,
			},
		},
		{
			name: "PatchNotInt",
			give: "git version 2.39.c",
			wantErrs: []string{
				`bad patch version "c"`,
			},
		},
		{
			name: "TooManyParts",
			give: "git version 1.2.3.4",
			wantErrs: []string{
				`bad version "1.2.3.4"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseVersion(tt.give)
			require.Error(t, err)

			for _, wantErr := range tt.wantErrs {
				assert.ErrorContains(t, err, wantErr)
			}
		})
	}
}

func TestCompareVersion(t *testing.T) {
	tests := []struct {
		name string
		a, b Version
		want int
	}{
		{
			name: "MajorGreater",
			a:    Version{2, 0, 0},
			b:    Version{1, 0, 0},
			want: 1,
		},
		{
			name: "MajorLess",
			a:    Version{1, 0, 0},
			b:    Version{2, 0, 0},
			want: -1,
		},
		{
			name: "MajorGreaterMinorLess",
			a:    Version{2, 0, 0},
			b:    Version{1, 1, 0},
			want: 1,
		},
		{
			name: "MinorGreater",
			a:    Version{1, 1, 0},
			b:    Version{1, 0, 0},
			want: 1,
		},
		{
			name: "MinorLess",
			a:    Version{1, 0, 0},
			b:    Version{1, 1, 0},
			want: -1,
		},
		{
			name: "PatchGreater",
			a:    Version{1, 0, 1},
			b:    Version{1, 0, 0},
			want: 1,
		},
		{
			name: "PatchLess",
			a:    Version{1, 0, 0},
			b:    Version{1, 0, 1},
			want: -1,
		},
		{
			name: "Equal",
			a:    Version{1, 0, 0},
			b:    Version{1, 0, 0},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Compare(tt.b)

			assert.Equal(t, tt.want, got)
		})
	}
}

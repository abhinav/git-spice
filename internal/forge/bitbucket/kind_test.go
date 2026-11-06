package bitbucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKind_UnmarshalText(t *testing.T) {
	tests := []struct {
		name string
		give string
		want Kind
	}{
		{name: "Empty", give: "", want: KindAuto},
		{name: "Auto", give: "auto", want: KindAuto},
		{name: "Cloud", give: "cloud", want: KindCloud},
		{name: "DataCenter", give: "datacenter", want: KindDataCenter},
		{name: "DataCenterHyphen", give: "data-center", want: KindDataCenter},
		{name: "Server", give: "server", want: KindDataCenter},
		{name: "CloudMixedCase", give: "Cloud", want: KindCloud},
		{name: "DataCenterUpperCase", give: "DATACENTER", want: KindDataCenter},
		{name: "ServerMixedCase", give: "Server", want: KindDataCenter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Kind
			require.NoError(t, got.UnmarshalText([]byte(tt.give)))
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("Invalid", func(t *testing.T) {
		var kind Kind
		err := kind.UnmarshalText([]byte("bitbucket"))
		require.Error(t, err)
		assert.ErrorContains(t, err, `invalid value "bitbucket"`)
		assert.ErrorContains(t, err, "expected auto, cloud, or datacenter")
	})
}

func TestKind_MarshalText(t *testing.T) {
	tests := []struct {
		give Kind
		want string
	}{
		{KindAuto, "auto"},
		{KindCloud, "cloud"},
		{KindDataCenter, "datacenter"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := tt.give.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))

			var roundTripped Kind
			require.NoError(t, roundTripped.UnmarshalText(got))
			assert.Equal(t, tt.give, roundTripped)
		})
	}

	t.Run("Unknown", func(t *testing.T) {
		_, err := Kind(42).MarshalText()
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid value: 42")
	})
}

func TestKind_String(t *testing.T) {
	tests := []struct {
		give Kind
		want string
	}{
		{KindAuto, "auto"},
		{KindCloud, "cloud"},
		{KindDataCenter, "datacenter"},
		{Kind(42), "Kind(42)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.String())
		})
	}
}

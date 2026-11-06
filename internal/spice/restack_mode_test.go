package spice

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestackMode_UnmarshalText(t *testing.T) {
	tests := []struct {
		give string
		want RestackMode
	}{
		{"none", RestackNone},
		{"false", RestackNone},
		{"upstack", RestackUpstack},
		{"true", RestackUpstack},
		{"aboves", RestackAboves},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			var got RestackMode
			require.NoError(t, got.UnmarshalText([]byte(tt.give)))
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("Invalid", func(t *testing.T) {
		var mode RestackMode
		err := mode.UnmarshalText([]byte("invalid"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid value")
		assert.ErrorContains(t, err, "expected none, aboves, or upstack")
	})
}

func TestRestackMode_MarshalText(t *testing.T) {
	tests := []struct {
		give RestackMode
		want string
	}{
		{RestackNone, "none"},
		{RestackUpstack, "upstack"},
		{RestackAboves, "aboves"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := tt.give.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}

	t.Run("Unknown", func(t *testing.T) {
		_, err := RestackMode(42).MarshalText()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid value: 42")
	})
}

func TestRestackMode_String(t *testing.T) {
	tests := []struct {
		give RestackMode
		want string
	}{
		{RestackNone, "none"},
		{RestackUpstack, "upstack"},
		{RestackAboves, "aboves"},
		{42, "RestackMode(42)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.String())
		})
	}
}

func TestRestackMode_Includes(t *testing.T) {
	assert.True(t, RestackNone.Includes(RestackNone))
	assert.False(t, RestackAboves.Includes(RestackNone))

	assert.True(t, RestackAboves.Includes(RestackAboves))
	assert.True(t, RestackUpstack.Includes(RestackAboves))
	assert.True(t, RestackUpstack.Includes(RestackUpstack))
	assert.False(t, RestackAboves.Includes(RestackUpstack))
}

package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClosedChanges_UnmarshalText(t *testing.T) {
	tests := []struct {
		give string
		want ClosedChanges
	}{
		{"ask", ClosedChangesAsk},
		{"ignore", ClosedChangesIgnore},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			var got ClosedChanges
			require.NoError(t, got.UnmarshalText([]byte(tt.give)))
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("Invalid", func(t *testing.T) {
		var c ClosedChanges
		err := c.UnmarshalText([]byte("invalid"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid value")
		assert.ErrorContains(t, err, "expected 'ask' or 'ignore'")
	})
}

func TestClosedChanges_MarshalText(t *testing.T) {
	tests := []struct {
		give ClosedChanges
		want string
	}{
		{ClosedChangesAsk, "ask"},
		{ClosedChangesIgnore, "ignore"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := tt.give.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}

	t.Run("Unknown", func(t *testing.T) {
		c := ClosedChanges(42)
		_, err := c.MarshalText()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid value: 42")
	})
}

func TestClosedChanges_String(t *testing.T) {
	tests := []struct {
		give ClosedChanges
		want string
	}{
		{ClosedChangesAsk, "ask"},
		{ClosedChangesIgnore, "ignore"},
		{42, "ClosedChanges(42)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.String())
		})
	}
}

func TestClosedChanges_RoundTrip(t *testing.T) {
	tests := []struct {
		give string
		want ClosedChanges
	}{
		{"ask", ClosedChangesAsk},
		{"ignore", ClosedChangesIgnore},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			// Unmarshal -> Marshal -> Unmarshal
			var c1 ClosedChanges
			require.NoError(t, c1.UnmarshalText([]byte(tt.give)))
			assert.Equal(t, tt.want, c1)

			marshaled, err := c1.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.give, string(marshaled))

			var c2 ClosedChanges
			require.NoError(t, c2.UnmarshalText(marshaled))
			assert.Equal(t, c1, c2)
		})
	}
}

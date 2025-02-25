package forge_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func TestRegister(t *testing.T) {
	var registry forge.Registry
	defer registry.Register(stubForge{
		id:      "a",
		baseURL: "https://example.com",
	})()

	t.Run("All", func(t *testing.T) {
		var ok bool
		for f := range registry.All() {
			if f.ID() == "a" {
				ok = true
				break
			}
		}
		assert.True(t, ok, "forge not found")
	})

	t.Run("Lookup", func(t *testing.T) {
		f, ok := registry.Lookup("a")
		assert.True(t, ok, "forge not found")
		assert.Equal(t, "a", f.ID(), "forge ID mismatch")

		t.Run("NotFound", func(t *testing.T) {
			_, ok := registry.Lookup("b")
			assert.False(t, ok, "unexpected forge match")
		})
	})

	t.Run("MatchForgeURL", func(t *testing.T) {
		f, ok := forge.MatchForgeURL(&registry, "https://example.com/foo")
		assert.True(t, ok, "forge not found")
		assert.Equal(t, "a", f.ID(), "forge ID mismatch")

		t.Run("NoMatch", func(t *testing.T) {
			_, ok := forge.MatchForgeURL(&registry, "https://example.org/foo")
			assert.False(t, ok, "unexpected forge match")
		})
	})
}

type stubForge struct {
	forge.Forge

	id      string
	baseURL string
}

func (f stubForge) ID() string {
	return f.id
}

func (f stubForge) MatchURL(remoteURL string) bool {
	return strings.HasPrefix(remoteURL, f.baseURL+"/")
}

func TestChangeState(t *testing.T) {
	tests := []struct {
		state forge.ChangeState
		str   string
	}{
		{forge.ChangeOpen, "open"},
		{forge.ChangeClosed, "closed"},
		{forge.ChangeMerged, "merged"},
	}

	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			t.Run("String", func(t *testing.T) {
				assert.Equal(t, tt.str, tt.state.String())
			})

			t.Run("MarshalRoundTrip", func(t *testing.T) {
				bs, err := tt.state.MarshalText()
				assert.NoError(t, err)

				var s forge.ChangeState
				require.NoError(t, s.UnmarshalText(bs))

				assert.Equal(t, tt.state, s)
			})
		})
	}

	t.Run("unknown", func(t *testing.T) {
		s := forge.ChangeState(42)

		t.Run("String", func(t *testing.T) {
			assert.Equal(t, "unknown", s.String())
		})

		t.Run("Marshal", func(t *testing.T) {
			_, err := s.MarshalText()
			assert.Error(t, err)
		})

		t.Run("Unmarshal", func(t *testing.T) {
			err := s.UnmarshalText([]byte("unknown"))
			assert.Error(t, err)
		})
	})
}

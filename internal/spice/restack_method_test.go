package spice

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestackMethod_UnmarshalText(t *testing.T) {
	tests := []struct {
		give string
		want RestackMethod
	}{
		{"rebase", RestackMethodRebase},
		{"", RestackMethodRebase},
		{"merge", RestackMethodMerge},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			var got RestackMethod
			require.NoError(t, got.UnmarshalText([]byte(tt.give)))
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("Invalid", func(t *testing.T) {
		var method RestackMethod
		err := method.UnmarshalText([]byte("invalid"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid value")
		assert.ErrorContains(t, err, "expected rebase or merge")
	})
}

func TestRestackMethod_MarshalText(t *testing.T) {
	tests := []struct {
		give RestackMethod
		want string
	}{
		{RestackMethodRebase, "rebase"},
		{RestackMethodMerge, "merge"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := tt.give.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}

	t.Run("Unknown", func(t *testing.T) {
		_, err := RestackMethod(42).MarshalText()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid value: 42")
	})
}

func TestRestackMethod_String(t *testing.T) {
	tests := []struct {
		give RestackMethod
		want string
	}{
		{RestackMethodRebase, "rebase"},
		{RestackMethodMerge, "merge"},
		{42, "RestackMethod(42)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.String())
		})
	}
}

func TestMergeAutoResolve_UnmarshalText(t *testing.T) {
	tests := []struct {
		give string
		want MergeAutoResolve
	}{
		{"none", MergeAutoResolveNone},
		{"", MergeAutoResolveNone},
		{"ours", MergeAutoResolveOurs},
		{"theirs", MergeAutoResolveTheirs},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			var got MergeAutoResolve
			require.NoError(t, got.UnmarshalText([]byte(tt.give)))
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("Invalid", func(t *testing.T) {
		var resolve MergeAutoResolve
		err := resolve.UnmarshalText([]byte("invalid"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid value")
		assert.ErrorContains(t, err, "expected none, ours, or theirs")
	})
}

func TestMergeAutoResolve_MarshalText(t *testing.T) {
	tests := []struct {
		give MergeAutoResolve
		want string
	}{
		{MergeAutoResolveNone, "none"},
		{MergeAutoResolveOurs, "ours"},
		{MergeAutoResolveTheirs, "theirs"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := tt.give.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}

	t.Run("Unknown", func(t *testing.T) {
		_, err := MergeAutoResolve(42).MarshalText()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid value: 42")
	})
}

func TestMergeAutoResolve_String(t *testing.T) {
	tests := []struct {
		give MergeAutoResolve
		want string
	}{
		{MergeAutoResolveNone, "none"},
		{MergeAutoResolveOurs, "ours"},
		{MergeAutoResolveTheirs, "theirs"},
		{42, "MergeAutoResolve(42)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.String())
		})
	}
}

func TestMergeAutoResolve_StrategyOption(t *testing.T) {
	tests := []struct {
		give MergeAutoResolve
		want string
	}{
		{MergeAutoResolveNone, ""},
		{MergeAutoResolveOurs, "ours"},
		{MergeAutoResolveTheirs, "theirs"},
	}

	for _, tt := range tests {
		t.Run(tt.give.String(), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.StrategyOption())
		})
	}
}

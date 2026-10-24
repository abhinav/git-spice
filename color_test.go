package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/git"
)

func TestParseColorValue(t *testing.T) {
	t.Run("On", func(t *testing.T) {
		for _, value := range []string{"true", "yes", "on", "1", "always", "TRUE", " Always "} {
			assert.Equal(t, colorOn, parseColorValue(value), "value %q", value)
		}
	})

	t.Run("Off", func(t *testing.T) {
		for _, value := range []string{"false", "no", "off", "0", "never", "False", "NEVER"} {
			assert.Equal(t, colorOff, parseColorValue(value), "value %q", value)
		}
	})

	t.Run("Unset", func(t *testing.T) {
		for _, value := range []string{"auto", "", "  ", "sometimes"} {
			assert.Equal(t, colorUnset, parseColorValue(value), "value %q", value)
		}
	})
}

func TestResolveColorPreference(t *testing.T) {
	configEnv := func(kvs ...string) []string {
		// Feed git configuration to the git command
		// through the environment, so the test needs no repository.
		if len(kvs)%2 != 0 {
			t.Fatal("configEnv needs key-value pairs")
		}
		var env []string
		for i := 0; i < len(kvs); i += 2 {
			n := string(rune('0' + i/2))
			env = append(env,
				"GIT_CONFIG_KEY_"+n+"="+kvs[i],
				"GIT_CONFIG_VALUE_"+n+"="+kvs[i+1],
			)
		}
		env = append(env, "GIT_CONFIG_COUNT="+string(rune('0'+len(kvs)/2)))
		return env
	}

	tests := []struct {
		name string
		env  []string
		want colorPreference
	}{
		{name: "NoConfig", want: colorUnset},
		{name: "ColorUIFalse", env: configEnv("color.ui", "false"), want: colorOff},
		{name: "ColorUINever", env: configEnv("color.ui", "never"), want: colorOff},
		{name: "ColorUITrue", env: configEnv("color.ui", "true"), want: colorOn},
		{name: "ColorUIAuto", env: configEnv("color.ui", "auto"), want: colorUnset},
		{name: "SpiceColorFalse", env: configEnv("spice.color", "false"), want: colorOff},
		{
			name: "SpiceColorOverridesColorUI",
			env:  configEnv("color.ui", "false", "spice.color", "always"),
			want: colorOn,
		},
		{
			name: "SpiceColorOverridesColorUIReverse",
			env:  configEnv("color.ui", "always", "spice.color", "never"),
			want: colorOff,
		},
		{
			name: "SpiceColorAutoOverridesColorUINever",
			env:  configEnv("color.ui", "never", "spice.color", "auto"),
			want: colorUnset,
		},
		{
			name: "SpiceColorAutoOverridesColorUIAlways",
			env:  configEnv("color.ui", "always", "spice.color", "auto"),
			want: colorUnset,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := git.NewConfig(git.ConfigOptions{Env: tt.env})
			assert.Equal(t, tt.want, resolveColorPreference(t.Context(), cfg))
		})
	}
}

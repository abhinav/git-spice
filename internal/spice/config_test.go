package spice_test

import (
	"context"
	"os"
	"path/filepath"
	reflect "reflect"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/logtest"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
)

func TestIntegrationConfig_loadFromGit(t *testing.T) {
	// Prevent current user's gitconfig from interfering with the test.
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	tests := []struct {
		name   string
		config string
		args   []string
		want   any

		wantErr []string // non-empty if error messages are expected
	}{
		{name: "Empty", want: struct {
			String  string `config:"string"`
			Integer int    `config:"integer"`
			Bool    bool   `config:"bool"`
		}{}},

		{
			name: "Configured",
			config: text.Dedent(`
				[spice]
				string = foo
				integer = 42
				bool = true
			`),
			want: struct {
				String  string `config:"string"`
				Integer int    `config:"integer"`
				Bool    bool   `config:"bool"`
			}{
				String:  "foo",
				Integer: 42,
				Bool:    true,
			},
		},
		{
			name: "Configured/Override",
			args: []string{"--string=bar"},
			config: text.Dedent(`
				[spice]
				string = foo
				integer = 42
			`),
			want: struct {
				String  string `config:"string"`
				Integer int    `config:"integer"`
				Bool    bool   `config:"bool"`
			}{String: "bar", Integer: 42},
		},

		{
			name: "Enum/Flag",
			want: struct {
				Level string `config:"level" enum:"mild,medium,hot" required:""`
			}{Level: "medium"},
			args: []string{"--level=medium"},
		},
		{
			name: "Enum/Config",
			want: struct {
				Level string `config:"level" enum:"mild,medium,hot" required:""`
			}{Level: "hot"},
			config: text.Dedent(`
				[spice]
				level = hot
			`),
		},
		{
			name: "Enum/ConfigInvalid",
			config: text.Dedent(`
				[spice]
				level = unknown
			`),
			want: struct {
				Level string `config:"level" enum:"mild,medium,hot" required:""`
			}{},
			wantErr: []string{`--level must be one of`, `got "unknown"`},
		},

		{
			name: "Multiple",
			config: text.Dedent(`
				[spice.include]
				path = foo
				path = bar
			`),
			want: struct {
				Include []string `config:"include.path"`
			}{Include: []string{"foo", "bar"}},
		},
		{
			name: "Multiple/Empty",
			want: struct {
				Include []string `config:"include.path"`
			}{},
		},
		{
			name: "Multiple/Override",
			args: []string{"--include=foo", "--include=bar"},
			config: text.Dedent(`
				[spice.include]
				path = baz
			`),
			want: struct {
				Include []string `config:"include.path"`
			}{Include: []string{"foo", "bar"}},
		},
		{
			name: "Multiple/Ints",
			config: text.Dedent(`
				[spice.include]
				path = 1
				path = 2
			`),
			want: struct {
				Include []int `config:"include.path"`
			}{Include: []int{1, 2}},
		},
		{
			name: "Multiple/NoSeparator",
			config: text.Dedent(`
				[spice.include]
				path = foo
				path = bar
			`),
			want: struct {
				Include []string `config:"include.path" sep:"none"`
			}{},
			wantErr: []string{`multiple values but no separator`},
		},
		{
			name: "Multiple/LastWins",
			config: text.Dedent(`
				[spice]
				level = mild
				level = medium
				level = hot
			`),
			want: struct {
				Level string `config:"level"`
			}{Level: "hot"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Set up
			home := t.TempDir()
			require.NoError(t, os.WriteFile(
				filepath.Join(home, ".gitconfig"),
				[]byte(tt.config),
				0o600,
			), "write configuration file")

			// Read configuration
			ctx := context.Background()
			gitCfg := git.NewConfig(git.ConfigOptions{
				Log: logtest.New(t),
				Dir: home,
				Env: []string{
					"HOME=" + home,
					"USER=testuser",
					"GIT_CONFIG_NOSYSTEM=1",
				},
			})
			spicecfg, err := spice.LoadConfig(ctx, gitCfg, spice.ConfigOptions{
				Log: logtest.New(t),
			})
			require.NoError(t, err, "load configuration")

			// Parse flags
			gotptr := reflect.New(reflect.TypeOf(tt.want)) // *T
			cli, err := kong.New(
				gotptr.Interface(),
				kong.Resolvers(spicecfg),
			)
			require.NoError(t, err, "create app")

			_, err = cli.Parse(tt.args)
			if len(tt.wantErr) > 0 {
				require.Error(t, err, "parse flags")
				for _, msg := range tt.wantErr {
					assert.ErrorContains(t, err, msg)
				}
				return
			}

			require.NoError(t, err, "parse flags")
			assert.Equal(t, tt.want, gotptr.Elem().Interface())
		})
	}
}

package spice_test

import (
	"os"
	"path/filepath"
	reflect "reflect"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
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

		shorthands map[string][]string

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
		{
			name: "Shorthands",
			config: text.Dedent(`
				[spice.shorthand]
				can = commit amend --no-edit
				wip = commit create -m \"wip: \\\"quoted\\\"\"
				poorly = \"  # ignored (not a valid shorthand)
			`),
			want: struct{}{},
			shorthands: map[string][]string{
				"can": {"commit", "amend", "--no-edit"},
				"wip": {"commit", "create", "-m", `wip: "quoted"`},
			},
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
			ctx := t.Context()
			gitCfg := git.NewConfig(git.ConfigOptions{
				Log: silogtest.New(t),
				Dir: home,
				Env: []string{
					"HOME=" + home,
					"USER=testuser",
					"GIT_CONFIG_NOSYSTEM=1",
				},
			})
			spicecfg, err := spice.LoadConfig(ctx, gitCfg, spice.ConfigOptions{
				Log: silogtest.New(t),
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

			gotShorthands := make(map[string][]string)
			for _, shorthand := range spicecfg.Shorthands() {
				longform, ok := spicecfg.ExpandShorthand(shorthand)
				require.True(t, ok, "expand(%q)", shorthand)
				gotShorthands[shorthand] = longform
			}

			if tt.shorthands == nil {
				tt.shorthands = make(map[string][]string)
			}

			assert.Equal(t, tt.shorthands, gotShorthands)
		})
	}
}

func TestConfig_ShellCommand(t *testing.T) {
	tests := []struct {
		name   string
		config string
		give   string
		want   string
	}{
		{
			name:   "Empty",
			config: "",
			give:   "foo",
		},
		{
			name: "SingleCommand",
			config: text.Dedent(`
				[spice.shorthand]
				foo = !echo hello
			`),
			give: "foo",
			want: "echo hello",
		},
		{
			name: "MultipleCommands",
			config: text.Dedent(`
				[spice.shorthand]
				foo = !echo hello
				bar = !ls -la
				baz = !git status
			`),
			give: "bar",
			want: "ls -la",
		},
		{
			name: "CommandNotFound",
			config: text.Dedent(`
				[spice.shorthand]
				foo = !echo hello
			`),
			give: "bar",
		},
		{
			name: "MixedShorthandsAndCommands",
			config: text.Dedent(`
				[spice.shorthand]
				can = commit amend --no-edit
				foo = !echo hello
				wip = commit create -m "wip"
				bar = !ls -la
			`),
			give: "foo",
			want: "echo hello",
		},
		{
			name: "ComplexShellCommand",
			config: text.Dedent(`
				[spice.shorthand]
				deploy = !git push origin main && echo Deployed
			`),
			give: "deploy",
			want: `git push origin main && echo Deployed`,
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
			ctx := t.Context()
			gitCfg := git.NewConfig(git.ConfigOptions{
				Log: silogtest.New(t),
				Dir: home,
				Env: []string{
					"HOME=" + home,
					"USER=testuser",
					"GIT_CONFIG_NOSYSTEM=1",
				},
			})
			spicecfg, err := spice.LoadConfig(ctx, gitCfg, spice.ConfigOptions{
				Log: silogtest.New(t),
			})
			require.NoError(t, err, "load configuration")

			gotCmd, ok := spicecfg.ShellCommand(tt.give)
			if tt.want == "" {
				require.False(t, ok, "ShellCommand(%q) unexpectedly found", tt.give)
			} else {
				require.True(t, ok, "ShellCommand(%q)", tt.give)
				assert.Equal(t, tt.want, gotCmd, "ShellCommand(%q) command", tt.give)
			}
		})
	}
}

func TestIntegrationConfig_gitConfigReferences(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	tests := []struct {
		name   string
		config string
		want   any
	}{
		{
			name: "GitConfigReference",
			config: text.Dedent(`
				[core]
				autocrlf = false
				editor = vim
				[spice]
				level = hot
			`),
			want: struct {
				AutoCRLF string `config:"@core.autocrlf"`
				Editor   string `config:"@core.editor"`
				Level    string `config:"level"`
			}{
				AutoCRLF: "false",
				Editor:   "vim",
				Level:    "hot",
			},
		},
		{
			name: "MixedReferences",
			config: text.Dedent(`
				[core]
				autocrlf = true
				safecrlf = warn
				[spice]
				string = foo
				integer = 42
			`),
			want: struct {
				AutoCRLF bool   `config:"@core.autocrlf"`
				SafeCRLF string `config:"@core.safecrlf"`
				String   string `config:"string"`
				Integer  int    `config:"integer"`
			}{
				AutoCRLF: true,
				SafeCRLF: "warn",
				String:   "foo",
				Integer:  42,
			},
		},
		{
			name: "GitConfigOnly",
			config: text.Dedent(`
				[core]
				autocrlf = true
				editor = emacs
			`),
			want: struct {
				AutoCRLF   string `config:"@core.autocrlf"`
				Editor     string `config:"@core.editor"`
				SpiceValue string `config:"spiceValue"`
			}{
				AutoCRLF:   "true",
				Editor:     "emacs",
				SpiceValue: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			require.NoError(t, os.WriteFile(
				filepath.Join(home, ".gitconfig"),
				[]byte(tt.config),
				0o600,
			), "write configuration file")

			ctx := t.Context()
			gitCfg := git.NewConfig(git.ConfigOptions{
				Log: silogtest.New(t),
				Dir: home,
				Env: []string{
					"HOME=" + home,
					"USER=testuser",
					"GIT_CONFIG_NOSYSTEM=1",
				},
			})
			spicecfg, err := spice.LoadConfig(ctx, gitCfg, spice.ConfigOptions{
				Log: silogtest.New(t),
			})
			require.NoError(t, err, "load configuration")

			gotptr := reflect.New(reflect.TypeOf(tt.want))
			cli, err := kong.New(
				gotptr.Interface(),
				kong.Resolvers(spicecfg),
			)
			require.NoError(t, err, "create app")

			_, err = cli.Parse([]string{})
			require.NoError(t, err, "parse flags")
			assert.Equal(t, tt.want, gotptr.Elem().Interface())
		})
	}
}

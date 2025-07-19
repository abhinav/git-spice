package git

import (
	"bytes"
	"io"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.uber.org/mock/gomock"
)

func TestConfigKey(t *testing.T) {
	tests := []struct {
		give string

		canonical  string
		section    string
		subsection string
		name       string
	}{
		{
			give:      "key",
			canonical: "key",
			name:      "key",
		},
		{
			give:      "section.key",
			canonical: "section.key",
			section:   "section",
			name:      "key",
		},
		{
			give:       "section.subsection.key",
			canonical:  "section.subsection.key",
			section:    "section",
			subsection: "subsection",
			name:       "key",
		},
		{
			give:       "section.multiple.subsections.key",
			canonical:  "section.multiple.subsections.key",
			section:    "section",
			subsection: "multiple.subsections",
			name:       "key",
		},
		{
			give:       "mixedCaseSection.mixedCaseSubsection.mixedCaseKey",
			canonical:  "mixedcasesection.mixedCaseSubsection.mixedcasekey",
			section:    "mixedCaseSection",
			subsection: "mixedCaseSubsection",
			name:       "mixedCaseKey",
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			key := ConfigKey(tt.give)
			assert.Equal(t, tt.canonical, string(key.Canonical()))
			assert.Equal(t, tt.section, key.Section())
			assert.Equal(t, tt.subsection, key.Subsection())
			assert.Equal(t, tt.name, key.Name())
		})
	}
}

func TestConfigListRegexp(t *testing.T) {
	pair := func(key, value string) string {
		return key + "\n" + value
	}

	lines := func(lines ...string) string {
		var buf bytes.Buffer
		for _, line := range lines {
			buf.WriteString(line)
			buf.WriteByte(0)
		}
		return buf.String()
	}

	tests := []struct {
		name string
		give string
		want []ConfigEntry
	}{
		{name: "Empty"},

		{
			name: "Single",
			give: "user.name\nAlice",
			want: []ConfigEntry{{Key: "user.name", Value: "Alice"}},
		},
		{
			name: "Multiple",
			give: lines(
				pair("user.name", "Alice"),
				pair("user.email", "alice@example.com"),
			),
			want: []ConfigEntry{
				{Key: "user.name", Value: "Alice"},
				{Key: "user.email", Value: "alice@example.com"},
			},
		},
		{
			name: "EmptyLines",
			give: lines(
				pair("user.name", "Alice"),
				"",
				pair("user.email", "alice@example.com"),
			),
			want: []ConfigEntry{
				{Key: "user.name", Value: "Alice"},
				{Key: "user.email", Value: "alice@example.com"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			execer := NewMockExecer(gomock.NewController(t))
			execer.EXPECT().
				Start(gomock.Any()).
				Do(func(cmd *exec.Cmd) error {
					// Writes to the command's stdout
					// must happen in a goroutine
					// because otherwise the pipe will deadlock.
					go func() {
						_, err := io.WriteString(cmd.Stdout, tt.give)
						assert.NoError(t, err)
						assert.NoError(t, cmd.Stdout.(io.Closer).Close())
					}()
					return nil
				}).
				Return(nil)
			execer.EXPECT().
				Wait(gomock.Any()).
				Return(nil)

			cfg := NewConfig(ConfigOptions{
				Dir:  t.TempDir(),
				Log:  silogtest.New(t),
				exec: execer,
			})

			got, err := sliceutil.CollectErr(cfg.ListRegexp(t.Context(), "."))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIntegrationConfigListRegexp(t *testing.T) {
	tests := []struct {
		name string

		// Groups of arguments to pass to `git config`
		// to set up the configuration.
		// e.g. [["user.name", "Alice"], ["user.email", "alice@example.com"]]
		sets [][]string

		// The regular expression patterns to search for in the configuration.
		// If empty, tests single pattern behavior for backward compatibility.
		patterns []string

		// The regular expression to search for in the configuration.
		// Used when patterns is empty for backward compatibility.
		pattern string

		want []ConfigEntry
	}{
		{name: "Empty"},
		{
			name: "Matches",
			sets: [][]string{
				{"user.name", "Alice"},
				{"user.email", "alice@example.com"},
			},
			pattern: `^user\.`,
			want: []ConfigEntry{
				{Key: "user.name", Value: "Alice"},
				{Key: "user.email", Value: "alice@example.com"},
			},
		},
		{
			name: "NoMatches",
			sets: [][]string{
				{"user.name", "Alice"},
				{"user.email", "alice@example.com"},
			},
			pattern: `^foo\.`,
		},
		{
			// config fields that can have multiple values.
			name: "MultiValue",
			sets: [][]string{
				{"--add", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main"},
				{"--add", "remote.origin.fetch", "+refs/heads/feature:refs/remotes/origin/feature"},
				{"--add", "remote.origin.fetch", "+refs/heads/username/*:refs/remotes/origin/username/*"},
			},
			pattern: `^remote\.origin\.`,
			want: []ConfigEntry{
				{Key: "remote.origin.fetch", Value: "+refs/heads/main:refs/remotes/origin/main"},
				{Key: "remote.origin.fetch", Value: "+refs/heads/feature:refs/remotes/origin/feature"},
				{Key: "remote.origin.fetch", Value: "+refs/heads/username/*:refs/remotes/origin/username/*"},
			},
		},
		{
			name: "MultiLine",
			sets: [][]string{
				{"some.key", "value1\nvalue2\nvalue3"},
			},
			pattern: `^some\.`,
			want: []ConfigEntry{
				{Key: "some.key", Value: "value1\nvalue2\nvalue3"},
			},
		},
		{
			name: "MultiplePatterns",
			sets: [][]string{
				{"user.name", "Alice"},
				{"user.email", "alice@example.com"},
				{"core.editor", "vim"},
				{"core.autocrlf", "false"},
			},
			patterns: []string{`^user\.`, `^core\.`},
			want: []ConfigEntry{
				{Key: "user.name", Value: "Alice"},
				{Key: "user.email", Value: "alice@example.com"},
				{Key: "core.editor", Value: "vim"},
				{Key: "core.autocrlf", Value: "false"},
			},
		},
		{
			name: "SinglePatternInArray",
			sets: [][]string{
				{"user.name", "Alice"},
				{"user.email", "alice@example.com"},
				{"core.editor", "vim"},
			},
			patterns: []string{`^user\.`},
			want: []ConfigEntry{
				{Key: "user.name", Value: "Alice"},
				{Key: "user.email", Value: "alice@example.com"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			env := []string{
				"HOME=" + home,
				"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
				"GIT_CONFIG_NOSYSTEM=1",
			}

			ctx := t.Context()
			log := silogtest.New(t)
			for _, set := range tt.sets {
				args := append([]string{"config", "--global"}, set...)
				err := newGitCmd(ctx, log, args...).
					Dir(home).
					AppendEnv(env...).
					Run(_realExec)
				require.NoError(t, err, "git-config: %v", args)
			}

			cfg := NewConfig(ConfigOptions{
				Dir: home,
				Env: env,
				Log: log,
			})

			var got []ConfigEntry
			var err error
			if len(tt.patterns) > 0 {
				got, err = sliceutil.CollectErr(cfg.ListRegexp(ctx, tt.patterns...))
			} else {
				got, err = sliceutil.CollectErr(cfg.ListRegexp(ctx, tt.pattern))
			}
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

package git

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/logtest"
	"go.uber.org/mock/gomock"
)

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
				Log:  logtest.New(t),
				exec: execer,
			})

			iter, err := cfg.ListRegexp(context.Background(), ".")
			require.NoError(t, err)

			var got []ConfigEntry
			iter(func(entry ConfigEntry, err error) bool {
				require.NoError(t, err)
				got = append(got, entry)
				return true
			})

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

		// The regular expression to search for in the configuration.
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

			ctx := context.Background()
			log := logtest.New(t)
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
			iter, err := cfg.ListRegexp(ctx, tt.pattern)
			require.NoError(t, err)
			iter(func(entry ConfigEntry, err error) bool {
				require.NoError(t, err)
				got = append(got, entry)
				return true
			})

			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

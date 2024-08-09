package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/log"
)

// Config provides access to Git configuration in the current context.
type Config struct {
	log  *log.Logger
	dir  string
	env  []string
	exec execer
}

// ConfigOptions configures the behavior of a [Config].
type ConfigOptions struct {
	// Dir specifies the directory to run Git commands in.
	// Defaults to the current working directory if empty.
	Dir string

	// Env specifies additional environment variables
	// to set when running Git commands.
	Env []string

	// Log used for logging messages to the user.
	// If nil, no messages are logged.
	Log *log.Logger

	exec execer
}

// NewConfig builds a new [Config] object for accessing Git configuration.
func NewConfig(opts ConfigOptions) *Config {
	exec := opts.exec
	if exec == nil {
		exec = _realExec
	}

	if opts.Log == nil {
		opts.Log = log.New(io.Discard)
	}

	return &Config{
		log:  opts.Log,
		dir:  opts.Dir,
		env:  opts.Env,
		exec: exec,
	}
}

// ConfigKey is divided into three parts:
//
//	section.subsection.name
//
// subsection may be absent, or may be comprised of multiple parts.
// section and name are case-insensitive.
// subsection is case-sensitive.
type ConfigKey string

// Split splits the key into its three parts:
// section, subsection, and name.
func (k ConfigKey) Split() (section, subsection, name string) {
	idx := strings.LastIndex(string(k), ".")
	if idx == -1 {
		// "foo" => "", "", "foo"
		return "", "", string(k)
	}

	name = string(k[idx+1:])
	k = k[:idx]

	idx = strings.Index(string(k), ".")
	if idx == -1 {
		// "foo.bar" => "foo", "", "bar"
		return string(k), "", name
	}

	// "foo.bar.baz" => "foo", "bar", "baz"
	return string(k[:idx]), string(k[idx+1:]), name
}

// Canonical returns a canonicalized form of the key.
// As the section and name are case-insensitive, they are lowercased,
// and the subsection is left as-is.
func (k ConfigKey) Canonical() ConfigKey {
	section, subsection, name := k.Split()

	var buf strings.Builder
	if section != "" {
		buf.WriteString(strings.ToLower(section))
		buf.WriteByte('.')
	}
	if subsection != "" {
		buf.WriteString(subsection)
		buf.WriteByte('.')
	}
	buf.WriteString(strings.ToLower(name))
	return ConfigKey(buf.String())
}

// Section returns the section name for the key,
// or the key itself if it doesn't have a section.
func (k ConfigKey) Section() string {
	section, _, _ := k.Split()
	return section
}

// Subsection returns the subsection name for the key,
// or an empty string if it doesn't have a subsection.
func (k ConfigKey) Subsection() string {
	_, subsection, _ := k.Split()
	return subsection
}

// Name returns the name for the key.
func (k ConfigKey) Name() string {
	_, _, name := k.Split()
	return name
}

// ConfigEntry is a single key-value pair in Git configuration.
type ConfigEntry struct {
	Key   ConfigKey
	Value string
}

// ListRegexp lists all configuration entries that match the given pattern.
// If pattern is empty, '.' is used to match all entries.
func (cfg *Config) ListRegexp(ctx context.Context, pattern string) (
	func(yield func(ConfigEntry, error) bool),
	error,
) {
	if pattern == "" {
		pattern = "."
	}
	return cfg.list(ctx, "--get-regexp", pattern)
}

var _newline = []byte("\n")

func (cfg *Config) list(ctx context.Context, args ...string) (
	func(yield func(ConfigEntry, error) bool),
	error,
) {
	args = append([]string{"config", "--null"}, args...)
	cmd := newGitCmd(ctx, cfg.log, args...).
		Dir(cfg.dir).
		AppendEnv(cfg.env...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(cfg.exec); err != nil {
		return nil, fmt.Errorf("start git-config: %w", err)
	}

	log := cfg.log
	return func(yield func(ConfigEntry, error) bool) {
		// Always wait for the command to finish when this returns.
		// Ignore the error because git-config fails if there are no matches.
		// It's not an error for us if there are no matches.
		defer func() {
			_ = cmd.Wait(cfg.exec)
		}()

		// With the --null flag, output is in the form:
		//
		//	key1\nvalue1\0
		//	key2\nvalue2\0
		scan := bufio.NewScanner(stdout)
		scan.Split(scanNullDelimited)
		for scan.Scan() {
			entry := scan.Bytes()
			key, value, ok := bytes.Cut(entry, _newline)
			if !ok {
				log.Warnf("skipping invalid entry: %q", entry)
				continue
			}

			if !yield(ConfigEntry{
				Key:   ConfigKey(key),
				Value: string(value),
			}, nil) {
				return
			}
		}

		if err := scan.Err(); err != nil {
			_ = yield(ConfigEntry{}, fmt.Errorf("scan git-config output: %w", err))
		}
	}, nil
}

// scanNullDelimited is a bufio.SplitFunc that splits on null bytes.
func scanNullDelimited(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		// No trailing null byte. Unlikely but easy to handle.
		return len(data), data, nil
	}
	return 0, nil, nil // request more data
}

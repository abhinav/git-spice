package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"

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

// ConfigEntry is a single key-value pair in Git configuration.
type ConfigEntry struct {
	Key   string
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
				Key:   string(key),
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

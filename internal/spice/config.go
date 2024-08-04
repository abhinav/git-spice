package spice

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
)

const (
	_configKey             = "config"
	_configNamespace       = "spice"
	_configNamespacePrefix = _configNamespace + "."
)

// GitConfigLister provides access to git-config output.
type GitConfigLister interface {
	ListRegexp(context.Context, string) (
		func(yield func(git.ConfigEntry, error) bool),
		error,
	)
}

var _ GitConfigLister = (*git.Config)(nil)

// Config defines the git-spice configuration source.
// It can be passed into Kong as a [kong.Resolver] to fill in flag values.
//
// Configuration for git-spice is specified via git-config.
// These can be system, user, repository, or worktree-level.
//
// The configuration keys are read from the root namespace "spice"
// for keys in the CLI grammar tagged with the `config:"key"` tag.
// Flags that are not tagged with `config:"key"` are ignored for configuration.
//
// For example:
//
//	type someCmd struct {
//		Level string `config:"level"`
//	}
//
// This will read the configuration key "spice.level" from git-config.
//
//	[spice]
//	level = hot
//
// Values are decoded using the same mapper as the flag.
// For single-valued fields, if multiple values are found in the configuration,
// the last value is used.
// For slice fields, all values are combined.
//
// If a flag is passed on the CLI, it takes precedence over the configuration.
type Config struct {
	// items is a map from configuration key (without the "spice." prefix)
	// to list of values for that field.
	items map[string][]string
}

// ConfigOptions spceifies options for the [Config].
type ConfigOptions struct {
	// Log specifies the logger to use for logging.
	// Defaults to no logging.
	Log *log.Logger
}

// LoadConfig loads configuration from the provided [GitConfig].
func LoadConfig(ctx context.Context, cfg GitConfigLister, opts ConfigOptions) (*Config, error) {
	if opts.Log == nil {
		opts.Log = log.New(io.Discard)
	}

	entries, err := cfg.ListRegexp(ctx, `^`+_configNamespace+`\.`)
	if err != nil {
		return nil, fmt.Errorf("list configuration: %w", err)
	}

	items := make(map[string][]string)

	err = nil // TODO: use a range loop after Go 1.23
	entries(func(entry git.ConfigEntry, iterErr error) bool {
		if iterErr != nil {
			err = iterErr
			return false
		}

		key, ok := strings.CutPrefix(entry.Key, _configNamespacePrefix)
		if !ok {
			// This will never happen if git config --get-regexp
			// behaves correctly, but it's easy to handle.
			return true
		}

		items[key] = append(items[key], entry.Value)
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}

	return &Config{
		items: items,
	}, nil
}

// Validate checks if the configuration is valid for the given application.
// This is a no-op, as we allow unknown configuration keys.
func (*Config) Validate(*kong.Application) error { return nil }

// Resolve resolves the value for a flag from configuration.
func (c *Config) Resolve(kctx *kong.Context, parent *kong.Path, flag *kong.Flag) (interface{}, error) {
	key := flag.Tag.Get(_configKey)
	if key == "" {
		return nil, nil
	}

	values := c.items[key]
	switch len(values) {
	case 0:
		return nil, nil

	case 1:
		return values[0], nil

	default:
		if flag.IsSlice() {
			if flag.Tag.Sep != -1 {
				// If there are multiple values, and a separator is defined,
				// let Kong split the values.
				return kong.JoinEscaped(values, flag.Tag.Sep), nil
			}

			return nil, fmt.Errorf("key %q has multiple values but no separator is defined", key)
		}

		// Last value wins if there are multiple instances
		// for a single-valued flag.
		return values[len(values)-1], nil
	}
}

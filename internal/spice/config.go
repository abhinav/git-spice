package spice

import (
	"context"
	"fmt"
	"io"
	"iter"
	"sort"

	"github.com/alecthomas/kong"
	"github.com/buildkite/shellwords"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
)

const (
	_configTag           = "config"
	_configSection       = "spice"
	_shorthandSubsection = "shorthand"
	_configSectionPrefix = _configSection + "."
)

// GitConfigLister provides access to git-config output.
type GitConfigLister interface {
	ListRegexp(context.Context, string) iter.Seq2[git.ConfigEntry, error]
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
	items map[git.ConfigKey][]string

	// shorthands is a map from shorthand to the list of arguments
	// that that it expands to.
	shorthands map[string][]string
}

// ConfigOptions specifies options for the [Config].
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

	items := make(map[git.ConfigKey][]string)
	shorthands := make(map[string][]string)

	for entry, err := range cfg.ListRegexp(ctx, `^`+_configSection+`\.`) {
		if err != nil {
			return nil, fmt.Errorf("list configuration: %w", err)
		}

		key := entry.Key.Canonical()
		section, subsection, name := key.Split()
		if section != _configSection {
			// Ignore keys that are not in the spice namespace.
			// This will never happen if git config --get-regexp
			// behaves correctly, but it's easy to handle.
			continue
		}

		// Special-case: Everything under "spice.shorthand.*"
		// defines a shorthand.
		if subsection == _shorthandSubsection {
			short := name
			longform, err := shellwords.SplitPosix(entry.Value)
			if err != nil {
				opts.Log.Warn("skipping shorthand with invalid value",
					"shorthand", short,
					"value", entry.Value,
					"error", err,
				)
				continue
			}

			shorthands[short] = longform
			continue
		}

		items[key] = append(items[key], entry.Value)
	}

	return &Config{
		items:      items,
		shorthands: shorthands,
	}, nil
}

// ExpandShorthand returns the long form of a custom shorthand command.
// Returns false if the shorthand is not defined.
func (c *Config) ExpandShorthand(name string) ([]string, bool) {
	args, ok := c.shorthands[name]
	return args, ok
}

// Shorthands returns a sorted list of all defined shorthands.
func (c *Config) Shorthands() []string {
	shorthands := make([]string, 0, len(c.shorthands))
	for short := range c.shorthands {
		shorthands = append(shorthands, short)
	}
	sort.Strings(shorthands)
	return shorthands
}

// Validate checks if the configuration is valid for the given application.
// This is a no-op, as we allow unknown configuration keys.
func (*Config) Validate(*kong.Application) error { return nil }

// Resolve resolves the value for a flag from configuration.
func (c *Config) Resolve(_ *kong.Context, _ *kong.Path, flag *kong.Flag) (interface{}, error) {
	k := flag.Tag.Get(_configTag)
	if k == "" {
		return nil, nil
	}

	key := git.ConfigKey(_configSectionPrefix + k).Canonical()
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

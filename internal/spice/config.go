package spice

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/buildkite/shellwords"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
)

const (
	_configTag            = "config"
	_spiceSection         = "spice"
	_shorthandSubsection  = "shorthand"
	_experimentSubsection = "experiment"
)

// GitSections is a list of Git-owned sections
// that we read configuration from.
var GitSections = []string{
	"core",
}

// GitConfigLister provides access to git-config output.
type GitConfigLister interface {
	ListRegexp(context.Context, ...string) iter.Seq2[git.ConfigEntry, error]
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
//
// Configuration keys that start with '@'
// are references to regular Git configuration keys,
// and are loaded ignoring the "spice." prefix.
//
// For example:
//
//	type someCmd struct {
//		CommentString string `config:"@core.commentString"`
//	}
//
// This will read the configuration key "core.commentString" from git-config.
type Config struct {
	// items is a map from configuration key (without the "spice." prefix)
	// to list of values for that field.
	items map[git.ConfigKey][]string

	// shorthands is a map from shorthand to the list of arguments
	// that that it expands to.
	shorthands map[string][]string

	// shellCommands is a map from shorthand to shell command.
	// These will be run with 'sh -c',
	// allowing shorthands to call any shell command.
	shellCommands map[string]string

	// experiments is a set of enabled experimental features.
	experiments map[string]struct{}
}

// ConfigOptions specifies options for the [Config].
type ConfigOptions struct {
	// Log specifies the logger to use for logging.
	// Defaults to no logging.
	Log *silog.Logger
}

// LoadConfig loads configuration from the provided [GitConfig].
func LoadConfig(ctx context.Context, cfg GitConfigLister, opts ConfigOptions) (*Config, error) {
	if opts.Log == nil {
		opts.Log = silog.Nop()
	}

	items := make(map[git.ConfigKey][]string)
	shorthands := make(map[string][]string)
	shellCommands := make(map[string]string)
	experiments := make(map[string]struct{})

	sectionNames := make(map[string]struct{})
	sectionNames[_spiceSection] = struct{}{}
	for _, section := range GitSections {
		sectionNames[section] = struct{}{}
	}

	configPatterns := make([]string, 0, len(sectionNames))
	for section := range sectionNames {
		configPatterns = append(configPatterns, "^"+section+`\.`)
	}

	for entry, err := range cfg.ListRegexp(ctx, configPatterns...) {
		if err != nil {
			return nil, fmt.Errorf("list configuration: %w", err)
		}

		key := entry.Key.Canonical()
		section, subsection, name := key.Split()
		if _, ok := sectionNames[section]; !ok {
			// Ignore keys that are not in requested sections.
			// This will never happen if git config --get-regexp
			// behaves correctly, but it's easy to handle.
			continue
		}

		switch {
		case section == _spiceSection && subsection == _shorthandSubsection:
			// Everything under "spice.shorthand.*" defines a shorthand.
			short := name

			// "!foo" is used for shell commands.
			if cmd, ok := strings.CutPrefix(entry.Value, "!"); ok {
				shellCommands[short] = cmd
				continue
			}

			// Normal shorthands are split into arguments.
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

		case section == _spiceSection && subsection == _experimentSubsection:
			// Everything under "spice.experiment.*"
			// opts in or out of an experimental feature.

			enable, err := strconv.ParseBool(entry.Value)
			if err != nil {
				opts.Log.Warn("Skipping experiment with invalid value",
					"name", name,
					"value", entry.Value,
					"error", err,
				)
				continue
			}

			// Experiment names are case-insensitive.
			experiment := strings.ToLower(name)
			if enable {
				experiments[experiment] = struct{}{}
			} else {
				delete(experiments, experiment)
			}

		default:
			items[key] = append(items[key], entry.Value)
		}
	}

	return &Config{
		items:         items,
		shorthands:    shorthands,
		shellCommands: shellCommands,
		experiments:   experiments,
	}, nil
}

// ExperimentEnabled reports whether the given experimental feature is enabled.
func (c *Config) ExperimentEnabled(name string) bool {
	_, ok := c.experiments[strings.ToLower(name)]
	return ok
}

// ExpandShorthand returns the long form of a custom shorthand command.
// Returns false if the shorthand is not defined.
func (c *Config) ExpandShorthand(name string) ([]string, bool) {
	args, ok := c.shorthands[name]
	return args, ok
}

// ShellCommand returns a custom shell command, if defined.
// Returns false if the command is not defined.
func (c *Config) ShellCommand(name string) (string, bool) {
	cmd, ok := c.shellCommands[name]
	return cmd, ok
}

// Shorthands returns a sorted list of all defined shorthands.
func (c *Config) Shorthands() []string {
	return slices.Sorted(maps.Keys(c.shorthands))
}

// Validate checks if the configuration is valid for the given application.
// This is a no-op, as we allow unknown configuration keys.
func (*Config) Validate(*kong.Application) error { return nil }

// Resolve resolves the value for a flag from configuration.
func (c *Config) Resolve(_ *kong.Context, _ *kong.Path, flag *kong.Flag) (any, error) {
	k := flag.Tag.Get(_configTag)
	if k == "" {
		return nil, nil
	}

	var key git.ConfigKey
	if gitKey, ok := strings.CutPrefix(k, "@"); ok {
		key = git.ConfigKey(gitKey).Canonical()
	} else {
		// If the key does not start with '@',
		// it is a spice configuration key.
		key = git.ConfigKey(_spiceSection + "." + k).Canonical()
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

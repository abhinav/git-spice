// Package experiment adds support to the Kong CLI
// for marking commands as experimental.
//
// # Usage
//
// Embed [Check] in the root command struct.
//
//	type mainCmd struct {
//		experiment.Check
//
//		// ...
//	}
//
// Annotate experimental commands with `experiment:"name"` tags.
//
//	type mainCmd struct {
//		// ...
//
//		MyCmd myCmd `cmd:"" experiment:"my-experiment"`
//	}
//
// An [Enabler] must be bound to the Kong Context
// for us to check if the experiment is enabled.
package experiment

import (
	"fmt"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/silog"
)

// Enabler reports whether an experiment is enabled.
type Enabler interface {
	ExperimentEnabled(string) bool
}

// Check is embedded into a Kong command
// to check for experimental commands.
type Check struct{}

// AfterApply is called by Kong after parsing the command line.
func (*Check) AfterApply(kctx *kong.Context, log *silog.Logger, enabler Enabler) error {
	// If any of the commands in the path are experimental,
	// make sure that the experiment is enabled.
	for _, path := range kctx.Path {
		cmd := path.Command
		if cmd == nil {
			continue
		}

		experiment := cmd.Tag.Get("experiment")
		if experiment == "" {
			continue
		}

		if enabler.ExperimentEnabled(experiment) {
			continue
		}

		log.Errorf("Command is experimental: %s", cmd.FullPath())
		log.Error("Enable the experiment to use it:")
		log.Errorf("  git config spice.experiment.%s true", experiment)
		log.Errorf("Before you enable the experiment, please read:")
		log.Errorf("  https://abhinav.github.io/git-spice/cli/experiments/")
		return fmt.Errorf("experiment not enabled: %v", experiment)
	}
	return nil
}

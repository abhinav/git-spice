package gittest

import (
	"runtime"
	"strconv"
)

// DefaultConfig is the default Git configuration
// for all test repositories.
func DefaultConfig() Config {
	cfg := Config{
		"init.defaultBranch": "main",
		// Freeze what refs get decorated in the log output.
		"log.excludeDecoration": "refs/remotes/*/HEAD",
		"alias.graph":           "log --graph --decorate --oneline",
		"core.autocrlf":         "false",
	}

	// On Windows, increase the timeout for template lookups.
	if runtime.GOOS == "windows" {
		cfg["spice.submit.listTemplatesTimeout"] = "10s"
	}

	return cfg
}

// Config is a set of Git configuration values.
type Config map[string]string

// EnvMap generates a map of environment variable assignments that will have
// the same effect as setting these configuration values in a Git repository.
func (c Config) EnvMap() map[string]string {
	env := make(map[string]string, len(c))

	// We can set Git configuration values by setting
	// GIT_CONFIG_KEY_<n>, GIT_CONFIG_VALUE_<n> and GIT_CONFIG_COUNT.
	var numCfg int
	for k, v := range c {
		n := strconv.Itoa(numCfg)
		env["GIT_CONFIG_KEY_"+n] = k
		env["GIT_CONFIG_VALUE_"+n] = v
		numCfg++
	}
	env["GIT_CONFIG_COUNT"] = strconv.Itoa(numCfg)
	return env
}

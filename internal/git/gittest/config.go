package gittest

import (
	"sort"
	"strconv"
)

// DefaultConfig is the default Git configuration
// for all test repositories.
func DefaultConfig() Config {
	return Config{
		"init.defaultBranch": "main",
		// Freeze what refs get decorated in the log output.
		"log.excludeDecoration": "refs/remotes/*/HEAD",
		"alias.graph":           "log --graph --decorate --oneline",
		"core.autocrlf":         "false",
	}
}

// Config is a set of Git configuration values.
type Config map[string]string

// Env generates a list of environment variable assignments that will have
// the same effect as setting these configuration values in a Git repository.
// This is suitable for passing to exec.Cmd.Env.
func (c Config) Env() []string {
	m := c.EnvMap()
	env := make([]string, 0, len(m))
	for k, v := range m {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	return env
}

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

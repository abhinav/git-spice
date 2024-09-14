package gittest

import (
	"fmt"
	"os/exec"
)

// DefaultConfig is the default Git configuration
// for all test repositories.
func DefaultConfig() Config {
	return Config{
		"init.defaultBranch": "main",
		"alias.graph":        "log --graph --decorate --oneline",
		"core.autocrlf":      "false",
	}
}

// Config is a set of Git configuration values.
type Config map[string]string

// WriteTo writes the Git configuration to the given file,
// creating it if it does not exist.
func (cfg Config) WriteTo(path string) error {
	args := []string{"config", "--file", path}
	for k, v := range cfg {
		cmd := exec.Command("git", append(args, k, v)...)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("set %s: %w", k, err)
		}
	}
	return nil
}

// Package cli provides core CLI tools for git-spice.
package cli

import (
	"os"
	"path/filepath"
)

var _name = filepath.Base(os.Args[0])

// Name returns the name of the current binary.
func Name() string {
	return _name
}

// SetName sets the name of the current binary.
//
// This is used at startup to override the default name.
func SetName(name string) {
	_name = name
}

// Package shorthand implements support for shorthand commands for the
// git-spice CLI.
package shorthand

import (
	"slices"
)

// Source is a source of shorthand expansions.
type Source interface {
	// ExpandShorthand expands the given shorthand command
	// into a list of arguments.
	//
	// If the command is not a shorthand, it returns false.
	ExpandShorthand(string) ([]string, bool)
}

// Expand expands the given arguments using the given source repeatedly
// until there's nothing left to expand.
//
// A single pattern is expanded only once.
// That is, if "commit" is declared as shorthand for "commit --amend",
// we will expand the "commit" shorthand only once.
func Expand(src Source, args []string) []string {
	if len(args) == 0 {
		return args
	}

	seen := make(map[string]struct{}) // to prevent infinite loops
	expanded, ok := src.ExpandShorthand(args[0])
	for ok {
		seen[args[0]] = struct{}{}
		args = slices.Replace(args, 0, 1, expanded...)

		if len(args) == 0 {
			// Unlikely but possible that the shorthand
			// just no-ops the arguments.
			break
		}

		// Don't expand the same string twice.
		if _, done := seen[args[0]]; done {
			break
		}

		expanded, ok = src.ExpandShorthand(args[0])
	}

	return args
}

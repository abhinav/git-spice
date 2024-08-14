package shorthand

import (
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
)

// BuiltinSource is a source of shorthand expansions
// built into a Kong CLI based on command aliases.
//
// The shorthand for each command is built by joining
// the first alias at each level of the command hierarchy.
// For example, for:
//
//	branch (b) create (c)
//
// The shorthand would be "bc".
type BuiltinSource struct {
	items map[string]builtinShorthand
}

var _ Source = (*BuiltinSource)(nil)

type builtinShorthand struct {
	Expanded []string
	Command  *kong.Node
}

// NewBuiltin builds a new BuiltinSource from the given Kong application.
// It extracts the shorthands from the command aliases.
func NewBuiltin(app *kong.Application) (*BuiltinSource, error) {
	items := make(map[string]builtinShorthand)

	// For each leaf subcommand, define a combined shorthand alias.
	// For example, if the command was "branch (b) create (c)",
	// the shorthand would be "bc".
	// For commands with multiple aliases, only the first is used.
commands:
	for _, n := range app.Leaves(false) {
		if n.Type != kong.CommandNode || len(n.Aliases) == 0 {
			continue
		}

		var fragments []string
		for c := n; c != nil && c.Type == kong.CommandNode; c = c.Parent {
			if len(c.Aliases) < 1 {
				// The command has an alias, but one of its parents doesn't.
				// No alias to add in this case.
				continue commands
			}
			fragments = append(fragments, c.Aliases[0])
		}
		if len(fragments) < 2 {
			// If the command is already a single word, don't add an alias.
			continue
		}

		slices.Reverse(fragments)
		short := strings.Join(fragments, "")
		if other, ok := items[short]; ok {
			return nil, fmt.Errorf("shorthand %q for %v is already in use by %v", short, n.Path(), other.Command.Path())
		}

		items[short] = builtinShorthand{
			Expanded: fragments,
			Command:  n,
		}
	}

	return &BuiltinSource{items: items}, nil
}

// ExpandShorthand expands the given shorthand command.
func (s *BuiltinSource) ExpandShorthand(cmd string) ([]string, bool) {
	if short, ok := s.items[cmd]; ok {
		return short.Expanded, true
	}
	return nil, false
}

// Keys returns the list of shorthand keys in the source.
func (s *BuiltinSource) Keys() iter.Seq[string] {
	return maps.Keys(s.items)
}

// Node returns the command node for the given shorthand key
// or nil if it doesn't exist.
func (s *BuiltinSource) Node(key string) *kong.Node {
	if short, ok := s.items[key]; ok {
		return short.Command
	}
	return nil
}

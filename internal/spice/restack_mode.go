package spice

import (
	"encoding"
	"fmt"
	"strings"

	"github.com/alecthomas/kong"
)

// RestackMode specifies how a command should restack branches affected by an
// operation that moves or removes their downstack branches.
type RestackMode int

const (
	// RestackNone leaves surviving upstacks retargeted in state
	// for a later explicit restack.
	RestackNone RestackMode = 0
)

const (
	// RestackAboves restacks only surviving direct upstacks.
	RestackAboves RestackMode = 1 << iota

	restackUpstackBranches

	// RestackUpstack restacks each surviving direct upstack
	// plus everything above it.
	RestackUpstack = RestackAboves | restackUpstackBranches
)

var (
	_ kong.BoolMapperValue     = (*RestackMode)(nil)
	_ encoding.TextUnmarshaler = (*RestackMode)(nil)
	_ encoding.TextMarshaler   = RestackMode(0)
)

// Includes reports whether this mode includes all behavior in scope.
func (m RestackMode) Includes(scope RestackMode) bool {
	if scope == RestackNone {
		return m == RestackNone
	}
	return m&scope == scope
}

// Decode decodes RestackMode from a Kong flag value.
//
// It behaves like a bool flag for compatibility with legacy --restack flags:
// omitting the value means true/upstack,
// and explicit true/false values map to upstack/none.
func (m *RestackMode) Decode(ctx *kong.DecodeContext) error {
	if ctx.Scan.Peek().Type != kong.FlagValueToken {
		*m = RestackUpstack
		return nil
	}

	token := ctx.Scan.Pop()
	switch v := token.Value.(type) {
	case string:
		return m.UnmarshalText([]byte(v))
	case bool:
		if v {
			*m = RestackUpstack
		} else {
			*m = RestackNone
		}
		return nil
	default:
		return fmt.Errorf("expected restack mode but got %q (%T)", token.Value, token.Value)
	}
}

// IsBool reports that RestackMode can be used as a bool-like flag.
func (*RestackMode) IsBool() bool {
	return true
}

// UnmarshalText decodes RestackMode from text.
func (m *RestackMode) UnmarshalText(bs []byte) error {
	switch strings.ToLower(string(bs)) {
	case "none", "false", "0", "no":
		*m = RestackNone
	case "upstack", "true", "1", "yes":
		*m = RestackUpstack
	case "aboves":
		*m = RestackAboves
	default:
		return fmt.Errorf(
			"invalid value %q:"+
				" expected none, aboves, or upstack",
			bs,
		)
	}
	return nil
}

// MarshalText encodes RestackMode to text.
func (m RestackMode) MarshalText() ([]byte, error) {
	switch m {
	case RestackNone:
		return []byte("none"), nil
	case RestackUpstack:
		return []byte("upstack"), nil
	case RestackAboves:
		return []byte("aboves"), nil
	default:
		return nil, fmt.Errorf("invalid value: %d", int(m))
	}
}

// String returns the string representation of RestackMode.
func (m RestackMode) String() string {
	switch m {
	case RestackNone:
		return "none"
	case RestackUpstack:
		return "upstack"
	case RestackAboves:
		return "aboves"
	default:
		return fmt.Sprintf("RestackMode(%d)", int(m))
	}
}

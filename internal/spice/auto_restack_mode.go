package spice

import (
	"encoding"
	"fmt"
	"strings"

	"github.com/alecthomas/kong"
)

// AutoRestackMode controls whether a command automatically restacks upstack
// branches after changing the current branch.
type AutoRestackMode int

const (
	// AutoRestackUpstack restacks the affected branch's upstack.
	AutoRestackUpstack AutoRestackMode = iota

	// AutoRestackNone disables automatic restacking.
	AutoRestackNone
)

var (
	_ kong.BoolMapperValue     = (*AutoRestackMode)(nil)
	_ encoding.TextUnmarshaler = (*AutoRestackMode)(nil)
	_ encoding.TextMarshaler   = AutoRestackMode(0)
)

// None reports whether automatic restacking is disabled.
func (m AutoRestackMode) None() bool {
	return m == AutoRestackNone
}

// Decode decodes AutoRestackMode from a Kong flag value.
//
// It behaves like a bool flag for compatibility with boolean-style restack
// flags: omitting the value means true/upstack, and explicit true/false values
// map to upstack/none.
func (m *AutoRestackMode) Decode(ctx *kong.DecodeContext) error {
	if ctx.Scan.Peek().Type != kong.FlagValueToken {
		*m = AutoRestackUpstack
		return nil
	}

	token := ctx.Scan.Pop()
	switch v := token.Value.(type) {
	case string:
		return m.UnmarshalText([]byte(v))
	case bool:
		if v {
			*m = AutoRestackUpstack
		} else {
			*m = AutoRestackNone
		}
		return nil
	default:
		return fmt.Errorf("expected restack mode but got %q (%T)", token.Value, token.Value)
	}
}

// IsBool reports that AutoRestackMode can be used as a bool-like flag.
func (*AutoRestackMode) IsBool() bool {
	return true
}

// UnmarshalText decodes AutoRestackMode from text.
func (m *AutoRestackMode) UnmarshalText(bs []byte) error {
	switch strings.ToLower(string(bs)) {
	case "none", "false", "0", "no":
		*m = AutoRestackNone
	case "upstack", "true", "1", "yes":
		*m = AutoRestackUpstack
	default:
		return fmt.Errorf("invalid value %q: expected none or upstack", bs)
	}
	return nil
}

// MarshalText encodes AutoRestackMode to text.
func (m AutoRestackMode) MarshalText() ([]byte, error) {
	switch m {
	case AutoRestackNone:
		return []byte("none"), nil
	case AutoRestackUpstack:
		return []byte("upstack"), nil
	default:
		return nil, fmt.Errorf("invalid value: %d", int(m))
	}
}

// String returns the string representation of AutoRestackMode.
func (m AutoRestackMode) String() string {
	switch m {
	case AutoRestackNone:
		return "none"
	case AutoRestackUpstack:
		return "upstack"
	default:
		return fmt.Sprintf("AutoRestackMode(%d)", int(m))
	}
}

package bitbucket

import (
	"encoding"
	"fmt"
	"strings"
)

// Kind specifies which Bitbucket product a [Forge] talks to.
type Kind int

const (
	// KindAuto infers the product from the instance URL.
	KindAuto Kind = iota

	// KindCloud selects Bitbucket Cloud.
	KindCloud

	// KindDataCenter selects Bitbucket Data Center.
	KindDataCenter
)

var (
	_ encoding.TextUnmarshaler = (*Kind)(nil)
	_ encoding.TextMarshaler   = Kind(0)
)

// UnmarshalText decodes a Kind from text.
func (k *Kind) UnmarshalText(bs []byte) error {
	switch strings.ToLower(string(bs)) {
	case "", "auto":
		*k = KindAuto
	case "cloud":
		*k = KindCloud
	case "datacenter", "data-center", "server":
		*k = KindDataCenter
	default:
		return fmt.Errorf(
			"invalid value %q: expected auto, cloud, or datacenter", bs,
		)
	}
	return nil
}

// MarshalText encodes a Kind as text.
func (k Kind) MarshalText() ([]byte, error) {
	switch k {
	case KindAuto:
		return []byte("auto"), nil
	case KindCloud:
		return []byte("cloud"), nil
	case KindDataCenter:
		return []byte("datacenter"), nil
	default:
		return nil, fmt.Errorf("invalid value: %d", int(k))
	}
}

// String returns the string representation of the Kind.
func (k Kind) String() string {
	switch k {
	case KindAuto:
		return "auto"
	case KindCloud:
		return "cloud"
	case KindDataCenter:
		return "datacenter"
	default:
		return fmt.Sprintf("Kind(%d)", int(k))
	}
}

// isCloudHost reports whether host belongs to Bitbucket Cloud.
func isCloudHost(host string) bool {
	return host == "bitbucket.org" ||
		strings.HasSuffix(host, ".bitbucket.org")
}

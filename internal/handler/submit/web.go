package submit

import (
	"errors"

	"github.com/alecthomas/kong"
)

// OpenWeb defines options for the --web flag.
type OpenWeb int

const (
	// OpenWebNever indicates that CRs should not be opened in a browser.
	OpenWebNever OpenWeb = iota

	// OpenWebAlways indicates that CRs should always be opened in a browser.
	OpenWebAlways

	// OpenWebOnCreate indicates that CRs should be opened in a browser
	// only when they are first created.
	OpenWebOnCreate
)

func (w *OpenWeb) shouldOpen(created bool) bool {
	switch *w {
	case OpenWebAlways:
		return true
	case OpenWebOnCreate:
		return created
	case OpenWebNever:
		return false
	default:
		panic("unknown OpenWeb value")
	}
}

// Decode decodes CLI flags for OpenWeb.
// The following forms are supported:
//
//	--web=[true|false|created]
//	--web  // equivalent to --web=true
func (w *OpenWeb) Decode(ctx *kong.DecodeContext) error {
	token := ctx.Scan.Peek()
	switch token.Type {
	case kong.EOLToken:
		// "--web" is equivalent to "--web=true".
		*w = OpenWebAlways
		return nil

	case kong.FlagValueToken, kong.ShortFlagTailToken:
		token, err := ctx.Scan.PopValue("web")
		if err != nil {
			return err
		}

		switch token.String() {
		case "true", "yes", "1":
			*w = OpenWebAlways
		case "false", "no", "0":
			*w = OpenWebNever
		case "created":
			*w = OpenWebOnCreate
		default:
			return errors.New("must be one of: 'true', 'false', 'created'")
		}

	default:
		// Treat "--web foo" as if "foo" is a different argument.
		*w = OpenWebAlways
	}
	return nil
}

// IsBool returns true to indicate that OpenWeb is a boolean flag.
// This is needed for Kong to render its help correctly.
func (w OpenWeb) IsBool() bool { return true }

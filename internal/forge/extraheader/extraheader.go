// Package extraheader adds extra HTTP headers to forge API requests.
//
// Headers are configured per forge
// with the spice.forge.<forge>.httpHeader git configuration key,
// holding "Name: value" pairs.
// A value beginning with "!" is interpreted as a command
// whose stdout provides the header value,
// allowing tokens that must be fetched at runtime,
// such as Cloudflare Access tokens.
package extraheader

import (
	"context"
	"fmt"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/buildkite/shellwords"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
)

// Header is an extra HTTP header to attach to forge API requests.
type Header struct {
	// Name is the canonicalized header field name.
	Name string // required

	// Value is the header value.
	Value string // required
}

// Spec is an unresolved header specification:
// "Name: value", or "Name: !command"
// where the command's stdout provides the header value.
type Spec struct {
	// Name is the canonicalized header field name.
	Name string // required

	// Value is the static header value.
	// Mutually exclusive with Command.
	Value string

	// Command produces the header value on stdout when non-empty.
	// The first element is the command name,
	// the rest are its arguments.
	Command []string
}

// Parse parses a header specification of the form "Name: value"
// or "Name: !command".
//
// The header name is canonicalized
// (e.g. "cf-access-token" becomes "Cf-Access-Token").
func Parse(spec string) (Spec, error) {
	name, value, ok := strings.Cut(spec, ":")
	if !ok {
		return Spec{}, fmt.Errorf("parse header %q: missing ':' separator", spec)
	}

	name = textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name))
	value = strings.TrimSpace(value)
	switch {
	case name == "":
		return Spec{}, fmt.Errorf("parse header %q: empty header name", spec)
	case !validHeaderName(name):
		return Spec{}, fmt.Errorf("parse header %q: invalid header name %q", spec, name)
	case value == "":
		return Spec{}, fmt.Errorf("parse header %q: empty header value", spec)
	}

	if cmd, ok := strings.CutPrefix(value, "!"); ok {
		args, err := shellwords.SplitPosix(cmd)
		if err != nil {
			return Spec{}, fmt.Errorf("parse header %q: %w", spec, err)
		}
		if len(args) == 0 {
			return Spec{}, fmt.Errorf("parse header %q: empty command", spec)
		}

		return Spec{Name: name, Command: args}, nil
	}

	return Spec{Name: name, Value: value}, nil
}

// CommandRunner runs a command with the given arguments
// and reports its stdout.
type CommandRunner func(ctx context.Context, name string, args []string) (string, error)

// ExecRunner returns a [CommandRunner] that runs commands
// through [xec], logging their output to the given logger.
func ExecRunner(log *silog.Logger) CommandRunner {
	return func(ctx context.Context, name string, args []string) (string, error) {
		out, err := xec.Command(ctx, log, name, args...).Output()
		return string(out), err
	}
}

// Resolve parses header specifications into concrete headers.
//
// Command-backed specifications are run exactly once
// with the given [CommandRunner],
// and their trimmed stdout becomes the header value.
// run may be nil only if no specification uses a command.
func Resolve(ctx context.Context, specs []string, run CommandRunner, log *silog.Logger) ([]Header, error) {
	headers := make([]Header, 0, len(specs))
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		parsed, err := Parse(spec)
		if err != nil {
			return nil, err
		}

		if _, ok := seen[parsed.Name]; ok {
			return nil, fmt.Errorf("duplicate header %q", parsed.Name)
		}
		seen[parsed.Name] = struct{}{}

		value := parsed.Value
		if len(parsed.Command) > 0 {
			if run == nil {
				return nil, fmt.Errorf("header %q: no command runner", spec)
			}

			log.Debug("Resolving extra HTTP header",
				"header", parsed.Name,
				"command", parsed.Command,
			)

			value, err = run(ctx, parsed.Command[0], parsed.Command[1:])
			if err != nil {
				return nil, fmt.Errorf("header %q: %w", spec, err)
			}
			value = strings.TrimSpace(value)

			switch {
			case strings.ContainsAny(value, "\n\r"):
				return nil, fmt.Errorf(
					"header %q: command output must be a single line", spec)
			case value == "":
				return nil, fmt.Errorf(
					"header %q: command output is empty", spec)
			}
		}

		headers = append(headers, Header{Name: parsed.Name, Value: value})
		log.Debug("Attaching extra HTTP header", "header", parsed.Name)
	}

	return headers, nil
}

// Transport is an [http.RoundTripper] that attaches
// extra headers to every outgoing request.
//
// Headers attached here override headers of the same name
// set by inner transports.
type Transport struct {
	// Base sends the request after headers are attached.
	Base http.RoundTripper // required

	// Headers are attached to every request.
	Headers []Header // required
}

// RoundTrip implements [http.RoundTripper].
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for _, h := range t.Headers {
		req.Header.Set(h.Name, h.Value)
	}
	return t.Base.RoundTrip(req)
}

// Client returns an HTTP client that attaches the given headers
// to every outgoing request.
//
// If headers is empty, base is returned unchanged.
// If base is nil, [http.DefaultClient] is used;
// it is not modified.
func Client(base *http.Client, headers []Header) *http.Client {
	if len(headers) == 0 {
		return base
	}

	if base == nil {
		base = http.DefaultClient
	}

	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	client := *base
	client.Transport = &Transport{
		Base:    transport,
		Headers: headers,
	}
	return &client
}

// validHeaderName reports whether the given canonicalized name
// consists only of valid header field name (token) characters
// per RFC 9110 section 5.6.1.
func validHeaderName(name string) bool {
	for i := range len(name) {
		if !isTokenChar(name[i]) {
			return false
		}
	}
	return name != ""
}

// isTokenChar reports whether c may appear in a header field name.
func isTokenChar(c byte) bool {
	const excluded = `"(),/:;<=>?@[\]{}`
	return c > ' ' && c < '\x7f' && strings.IndexByte(excluded, c) < 0
}

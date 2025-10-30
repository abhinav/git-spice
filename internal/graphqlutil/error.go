// Package graphqlutil provides utilities for working with GraphQL.
package graphqlutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
	"go.abhg.dev/gs/internal/must"
)

// Common errors that may be returned by GraphQL APIs.
// These may be matched with errors.Is.
var (
	ErrNotFound      = errors.New("not found")
	ErrForbidden     = errors.New("forbidden")
	ErrUnprocessable = errors.New("unprocessable")
)

// graphQLTransport wraps an HTTP transport
// with an understanding of GraphQL errors.
//
// This is a hack to work around https://github.com/shurcooL/graphql/issues/31.
// In short, we can't get error codes back from the upstream GraphQL library,
// so we'll parse them ourselves at the transport level.
//
// If this ever becomes insufficient, we'll have to switch to a different
// GraphQL client.
type graphQLTransport struct {
	t http.RoundTripper
}

var _ http.RoundTripper = (*graphQLTransport)(nil)

// WrapTransport wraps an HTTP transport
// with knowledge of GraphQL errors.
//
// The transport will now return errors that may be cast to
// [Errors] or [Error] with errors.As.
func WrapTransport(t http.RoundTripper) http.RoundTripper {
	if t == nil {
		t = http.DefaultTransport
	}
	return &graphQLTransport{t: t}
}

// RoundTrip handles a single HTTP round trip.
func (t *graphQLTransport) RoundTrip(r *http.Request) (res *http.Response, err error) {
	res, err = t.t.RoundTrip(r)
	if err != nil || res.StatusCode != http.StatusOK {
		return res, err
	}

	buff := takeBuffer()
	defer func() {
		// If there was an error,
		// we're not using the buffer in the response
		// so return it to the pool now.
		if err != nil {
			putBuffer(buff)
		}
	}()

	// Read the entire response body into a buffer.
	_, readErr := io.Copy(buff, res.Body)
	closeErr := res.Body.Close()
	// As long as we return a nil error,
	// we need to replace the response body
	// so it can be read again.
	//
	// The pooledReadCloser will return the buffer to the pool
	// when it's closed.
	res.Body = &pooledReadCloser{
		Reader: bytes.NewReader(buff.Bytes()),
		buf:    buff,
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}

	// If the response contains a GraphQL error,
	// we'll want to parse it and return that instead.
	// gjson makes this relatively cheap to check before we parse.
	errs := gjson.GetBytes(buff.Bytes(), "errors")
	if !errs.IsArray() || !errs.Get("0").IsObject() {
		return res, nil
	}

	var gqlErrs Errors
	if err := json.Unmarshal([]byte(errs.Raw), &gqlErrs); err != nil {
		// This isn't a valid GraphQL error.
		// Return the original response.
		return res, nil
	}

	// gqlErrs cannot be empty because we wouldn't have gotten here
	// if there were no errors.
	must.NotBeEmptyf(gqlErrs, "expected at least one GraphQL error")
	return nil, gqlErrs
}

// Errors is a list of GraphQL errors.
type Errors []*Error

func (e Errors) Unwrap() []error {
	errs := make([]error, len(e))
	for i, err := range e {
		errs[i] = err
	}
	return errs
}

func (e Errors) Error() string {
	var s strings.Builder
	for i, err := range e {
		if i > 0 {
			s.WriteString("\n")
		}
		s.WriteString(err.Error())
	}
	return s.String()
}

// Error is a single GraphQL error.
// A single response may contain multiple errors.
type Error struct {
	Message string `json:"message"`
	Path    []any  `json:"path"`
	Type    string `json:"type"`
}

// Is reports whether this error matches the target error.
// Use errors.Is to match against this error.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.Type == "NOT_FOUND"
	case ErrForbidden:
		return e.Type == "FORBIDDEN"
	case ErrUnprocessable:
		return e.Type == "UNPROCESSABLE"
	default:
		return false
	}
}

func (e *Error) Error() string {
	var s strings.Builder
	if len(e.Path) > 0 {
		for i, p := range e.Path {
			if i > 0 {
				s.WriteString(".")
			}
			fmt.Fprintf(&s, "%v", p)
		}
		s.WriteString(": ")
	}
	if len(e.Type) > 0 {
		fmt.Fprintf(&s, "%s: ", e.Type)
	}
	s.WriteString(e.Message)
	return s.String()
}

// pooledReadCloser wraps a bytes.Reader with a buffer
// that gets returned to the pool when Close is called.
type pooledReadCloser struct {
	*bytes.Reader
	buf *bytes.Buffer
}

func (p *pooledReadCloser) Close() error {
	if p.buf != nil {
		putBuffer(p.buf)
		p.buf = nil
	}
	return nil
}

var _bufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func takeBuffer() *bytes.Buffer {
	buf := _bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func putBuffer(buf *bytes.Buffer) {
	_bufferPool.Put(buf)
}

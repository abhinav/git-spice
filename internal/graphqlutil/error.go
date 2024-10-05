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
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
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
// [ErrorList] or [Error] with errors.As.
func WrapTransport(t http.RoundTripper) http.RoundTripper {
	if t == nil {
		t = http.DefaultTransport
	}
	return &graphQLTransport{t: t}
}

// RoundTrip handles a single HTTP round trip.
func (t *graphQLTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := t.t.RoundTrip(r)
	if err != nil || res.StatusCode != http.StatusOK {
		return res, err
	}

	buff := takeBuffer()
	defer putBuffer(buff)

	// Read the entire response body into a buffer.
	_, readErr := io.Copy(buff, res.Body)
	closeErr := res.Body.Close()
	res.Body = io.NopCloser(bytes.NewReader(buff.Bytes()))
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

	var gqlErrs ErrorList
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

// ErrorList is a list of GraphQL errors.
type ErrorList []*Error

func (e ErrorList) Unwrap() []error {
	errs := make([]error, len(e))
	for i, err := range e {
		errs[i] = err
	}
	return errs
}

func (e ErrorList) Error() string {
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

var _bufferPool = sync.Pool{
	New: func() interface{} {
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

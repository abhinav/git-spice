package github

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors classify GitHub GraphQL error types.
// Match them with [errors.Is].
var (
	// ErrNotFound matches a GraphQL error whose type is NOT_FOUND.
	ErrNotFound = errors.New("not found")

	// ErrForbidden matches a GraphQL error whose type is FORBIDDEN.
	ErrForbidden = errors.New("forbidden")

	// ErrUnprocessable matches a GraphQL error whose type is UNPROCESSABLE.
	ErrUnprocessable = errors.New("unprocessable")
)

// Error is one error from a GitHub GraphQL response.
type Error struct {
	// Message is GitHub's human-readable description of the error.
	Message string `json:"message"`

	// Path identifies the response field where GitHub encountered the error.
	Path []any `json:"path"`

	// Type is GitHub's machine-readable error classification.
	Type string `json:"type"`
}

// Is reports whether GitHub's error type matches a package sentinel.
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

// Error formats the GraphQL path and type before GitHub's message when those
// fields are present.
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

type graphQLError []*Error

// Unwrap returns every error reported by GitHub.
func (e graphQLError) Unwrap() []error {
	errs := make([]error, len(e))
	for i, err := range e {
		errs[i] = err
	}
	return errs
}

// Error formats the errors in response order, one per line.
func (e graphQLError) Error() string {
	var s strings.Builder
	for i, err := range e {
		if i > 0 {
			s.WriteString("\n")
		}
		s.WriteString(err.Error())
	}
	return s.String()
}

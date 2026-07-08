package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrNotFound reports a Bitbucket Data Center 404 response.
var ErrNotFound = errors.New("404 Not Found")

// ErrConflict reports a Bitbucket Data Center 409 response, typically an
// optimistic-locking version mismatch.
var ErrConflict = errors.New("409 Conflict")

// APIErrorDetail is a single entry in the Bitbucket Data Center
// error envelope.
type APIErrorDetail struct {
	// Message is a human-readable, server-localized description.
	Message string `json:"message"`

	// ExceptionName is the Java exception class, often null on validation 400s.
	ExceptionName string `json:"exceptionName"`

	// Context is the request field a field-level validation error refers to.
	Context string `json:"context"`
}

// apiErrorEnvelope is the Bitbucket Data Center error envelope:
//
//	{"errors":[{"context":"...","message":"...","exceptionName":"..."}]}
type apiErrorEnvelope struct {
	Errors []APIErrorDetail `json:"errors"`
}

// APIError is an error returned for an unexpected HTTP status code.
// It carries the parsed Bitbucket Data Center error envelope
// when the response body contained one.
type APIError struct {
	StatusCode int
	Method     string
	URL        string

	// Details are the parsed error envelope entries, if any.
	Details []APIErrorDetail

	// Body is the raw response body.
	Body []byte
}

func (e *APIError) Error() string {
	if msg := e.message(); msg != "" {
		return fmt.Sprintf("%s %s: %d %s", e.Method, e.URL, e.StatusCode, msg)
	}
	if len(bytes.TrimSpace(e.Body)) == 0 {
		return fmt.Sprintf("%s %s: %d", e.Method, e.URL, e.StatusCode)
	}
	return fmt.Sprintf(
		"%s %s: %d %s",
		e.Method,
		e.URL,
		e.StatusCode,
		strings.TrimSpace(string(e.Body)),
	)
}

// message returns the joined messages from the parsed error envelope.
func (e *APIError) message() string {
	var msgs []string
	for _, d := range e.Details {
		if m := strings.TrimSpace(d.Message); m != "" {
			msgs = append(msgs, m)
		}
	}
	return strings.Join(msgs, "; ")
}

// parseAPIErrorEnvelope parses the Bitbucket Data Center error
// envelope from a response body, returning nil if it is absent
// or malformed.
func parseAPIErrorEnvelope(body []byte) []APIErrorDetail {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}

	var envelope apiErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}
	return envelope.Errors
}

// checkResponse converts unsuccessful HTTP responses into package errors.
//
// It reads the response body only when an error path needs response details.
func checkResponse(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK,
		http.StatusCreated,
		http.StatusAccepted,
		http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return ErrConflict
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read error response: %w", err)
	}

	details := parseAPIErrorEnvelope(body)
	return &APIError{
		StatusCode: resp.StatusCode,
		Method:     resp.Request.Method,
		URL:        resp.Request.URL.String(),
		Details:    details,
		Body:       body,
	}
}

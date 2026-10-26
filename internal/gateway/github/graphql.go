package github

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// compactGraphQL removes line indentation and line breaks from a GraphQL
// document while preserving the contents of each trimmed line.
func compactGraphQL(document string) string {
	var query strings.Builder
	for line := range strings.Lines(document) {
		query.WriteString(strings.TrimSpace(line))
	}
	return query.String()
}

// graphQLRequestEnvelope is the JSON request shape accepted by GitHub's
// GraphQL API.
// Variables remains operation-specific so typed operations can own their wire
// fields without exposing generic GraphQL execution to callers.
type graphQLRequestEnvelope struct {
	Query     string `json:"query"`
	Variables any    `json:"variables"`
}

// graphQLResponseEnvelope separates the GraphQL operation result from
// protocol-level errors before operation-specific decoding.
// Keeping Data raw ensures errors take precedence over partial data.
type graphQLResponseEnvelope struct {
	Data   jsontext.Value `json:"data"`
	Errors graphQLError   `json:"errors"`
}

// executeGQL performs the shared GraphQL request lifecycle for typed operations.
//
// The query and variables describe the operation's stable wire request.
// The result must be a pointer suitable for decoding the operation's data
// shape.
// executeGQL retrieves credentials for every call so dynamic token sources
// retain their request lifetime.
// It owns response-body closure and converts transport, HTTP, protocol, and
// data-shape failures into errors with stage-specific context.
// A non-empty GraphQL error list always wins over Data to preserve the client's
// all-or-error result contract.
func (c *Gateway) executeGQL(ctx context.Context, query string, variables, result any) error {
	var body bytes.Buffer
	if err := json.MarshalWrite(
		&body,
		graphQLRequestEnvelope{
			Query:     query,
			Variables: variables,
		},
		json.Deterministic(true),
	); err != nil {
		return fmt.Errorf("encode GraphQL request: %w", err)
	}

	req, err := c.newRequest(ctx, http.MethodPost, c.graphQLEndpoint, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send GraphQL request: %w", err)
	}
	// GraphQL envelopes are defined only for successful HTTP responses.
	// Bound diagnostics from other responses before closing the body so an
	// intermediary cannot make error reporting consume unbounded memory.
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		diagnostic, err := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
		err = errors.Join(err, res.Body.Close())
		if err != nil {
			return fmt.Errorf(
				"GitHub GraphQL HTTP status %s: read response: %w",
				res.Status,
				err,
			)
		}
		return fmt.Errorf(
			"GitHub GraphQL HTTP status %s: %s",
			res.Status,
			strings.TrimSpace(string(diagnostic)),
		)
	}

	responseBody, err := io.ReadAll(res.Body)
	err = errors.Join(err, res.Body.Close())
	if err != nil {
		return fmt.Errorf("read GraphQL response: %w", err)
	}

	var envelope graphQLResponseEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return fmt.Errorf("decode GraphQL response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return envelope.Errors
	}
	if err := json.Unmarshal(envelope.Data, result); err != nil {
		return fmt.Errorf("decode GraphQL data: %w", err)
	}
	return nil
}

func (c *Gateway) mutate(ctx context.Context, mutation string, input, result any) error {
	return c.executeGQL(ctx, mutation, struct {
		Input any `json:"input"`
	}{input}, result)
}

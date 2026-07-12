package github

import "strings"

// compactGraphQL removes line indentation and line breaks from a GraphQL
// document while preserving the contents of each trimmed line.
func compactGraphQL(document string) string {
	var query strings.Builder
	for line := range strings.Lines(document) {
		query.WriteString(strings.TrimSpace(line))
	}
	return query.String()
}

package spice

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/httpclient"
)

// HTTPHeaderRules resolves destination-scoped HTTP header configuration.
// headerFromEnv expands only header values using [os.Expand] with [os.Getenv].
// Environment values are read when this method is called.
func (c *Config) HTTPHeaderRules() ([]httpclient.HeaderRule, error) {
	if c == nil {
		return nil, nil
	}
	var rules []httpclient.HeaderRule
	for _, key := range slices.Sorted(maps.Keys(c.items)) {
		_, subsection, name := key.Split()
		rawOrigin, ok := strings.CutPrefix(subsection, "http.")
		if !ok || (name != "header" && name != "headerfromenv") {
			continue
		}
		origin, err := httpclient.ParseOrigin(rawOrigin)
		if err != nil {
			return nil, fmt.Errorf("configuration %q: %w", key, err)
		}
		for _, spec := range c.items[key] {
			headerName, value, ok := strings.Cut(spec, ":")
			if !ok {
				return nil, fmt.Errorf("configuration %q: expected a header name followed by a colon and value", key)
			}
			headerName = strings.Trim(headerName, " \t")
			value = strings.Trim(value, " \t")
			if name == "headerfromenv" {
				value = os.Expand(value, os.Getenv)
			}
			rule, err := httpclient.NewHeaderRule(origin, headerName, value)
			if err != nil {
				return nil, fmt.Errorf("configuration %q: %w", key, err)
			}
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

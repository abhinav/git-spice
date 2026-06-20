package server

import (
	"context"
	"iter"
	"maps"
	"net/url"
	"strconv"
)

// _defaultPageLimit is the page size requested when paginating collections.
const _defaultPageLimit = 100

// page is a single page of a paginated Bitbucket Data Center response,
// which uses start/limit offsets and an isLastPage flag.
type page[T any] struct {
	Values     []T  `json:"values"`
	IsLastPage bool `json:"isLastPage"`

	// NextPageStart is the start offset of the next page,
	// set only when IsLastPage is false.
	NextPageStart int `json:"nextPageStart"`
}

// getPaged returns an iterator over all items at path, walking Data Center
// pages until isLastPage. It is a free function because Go has no generic
// methods. Iteration stops on the first error, yielding the zero value of T.
func getPaged[T any](
	ctx context.Context,
	c *Client,
	path string,
	query url.Values,
) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		start := 0
		for {
			pageQuery := make(url.Values, len(query)+2)
			maps.Copy(pageQuery, query)
			pageQuery.Set("start", strconv.Itoa(start))
			if pageQuery.Get("limit") == "" {
				pageQuery.Set("limit", strconv.Itoa(_defaultPageLimit))
			}

			var p page[T]
			if _, err := c.get(ctx, path, pageQuery, &p); err != nil {
				var zero T
				yield(zero, err)
				return
			}

			for _, value := range p.Values {
				if !yield(value, nil) {
					return
				}
			}

			if p.IsLastPage || len(p.Values) == 0 {
				return
			}

			next := p.NextPageStart
			if next <= start {
				// Defend against a server that omits nextPageStart.
				next = start + len(p.Values)
			}
			start = next
		}
	}
}

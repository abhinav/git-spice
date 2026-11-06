package forgejo

import (
	"fmt"
	"net/url"
	"strconv"
)

// Forgejo API references:
// https://codeberg.org/api/swagger
// https://codeberg.org/swagger.v1.json

// ListOptions configures Forgejo page-based pagination.
type ListOptions struct {
	// Page selects the one-based result page.
	Page int64

	// Limit selects the maximum number of items per page.
	Limit int64
}

func (o *ListOptions) encodeQuery() url.Values {
	values := make(url.Values)
	if o == nil {
		return values
	}
	if o.Page > 0 {
		values.Set("page", strconv.FormatInt(o.Page, 10))
	}
	if o.Limit > 0 {
		values.Set("limit", strconv.FormatInt(o.Limit, 10))
	}
	return values
}

func repoPath(owner string, repo string) string {
	return fmt.Sprintf(
		"repos/%s/%s",
		url.PathEscape(owner),
		url.PathEscape(repo),
	)
}

package github

import "fmt"

// PaginationOptions configures the page requests behind a gateway iterator.
type PaginationOptions struct {
	// ItemsPerPage is the number of connection items requested from GitHub.
	// Zero selects the operation's default; other values must be from 1 through
	// 100.
	ItemsPerPage int
}

func paginationItemsPerPage(opts *PaginationOptions, defaultValue int) (int, error) {
	if opts == nil || opts.ItemsPerPage == 0 {
		return defaultValue, nil
	}
	if opts.ItemsPerPage < 1 || opts.ItemsPerPage > 100 {
		return 0, fmt.Errorf("items per page must be from 1 through 100: %d", opts.ItemsPerPage)
	}
	return opts.ItemsPerPage, nil
}

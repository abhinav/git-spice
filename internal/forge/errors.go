package forge

import "errors"

// ErrUnsupportedURL indicates that the given remote URL
// does not match any registered forge.
var ErrUnsupportedURL = errors.New("unsupported URL")

// ErrNotFound indicates that a requested resource does not exist.
var ErrNotFound = errors.New("not found")

// ErrCommentCannotUpdate indicates that an existing comment cannot be updated.
// This typically occurs when local state is missing required information
// (e.g., PR ID for Bitbucket comments).
// Callers should handle this by posting a new comment instead.
var ErrCommentCannotUpdate = errors.New("comment cannot be updated")

package forge

import "regexp"

// CommentFormat specifies formatting preferences for navigation comments.
type CommentFormat struct {
	// Footer is appended at the end of the navigation comment.
	// Defaults to HTML <sub> tag if empty.
	Footer string

	// Marker is an invisible marker used to identify navigation comments.
	// Defaults to HTML comment if empty.
	Marker string
}

// WithCommentFormat is an optional interface that forges can implement
// to customize navigation comment formatting.
// This is useful for forges like Bitbucket that don't support HTML in comments.
type WithCommentFormat interface {
	Forge

	// CommentFormat returns custom formatting for navigation comments.
	CommentFormat() CommentFormat
}

// ChangeCommentID is a unique identifier for a comment on a change.
type ChangeCommentID interface {
	String() string
}

// ListChangeCommentsOptions specifies options for filtering
// and limiting comments listed by ListChangeComments.
//
// Conditions specified here are combined with AND.
type ListChangeCommentsOptions struct {
	// BodyMatchesAll specifies zero or more regular expressions
	// that must all match the comment body.
	//
	// If empty, all comments are returned.
	BodyMatchesAll []*regexp.Regexp

	// CanUpdate specifies whether only comments that can be updated
	// by the current user should be returned.
	//
	// If false, all comments are returned.
	CanUpdate bool
}

// ListChangeCommentItem is a single result from listing comments on a change.
type ListChangeCommentItem struct {
	ID   ChangeCommentID
	Body string
}

// CommentCounts represents comment/thread resolution counts on a change.
type CommentCounts struct {
	// Total is the total number of resolvable comments or threads.
	Total int

	// Resolved is the number of resolved comments or threads.
	Resolved int

	// Unresolved is the number of unresolved comments or threads.
	Unresolved int
}

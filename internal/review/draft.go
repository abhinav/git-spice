package review

import "strconv"

// DraftID identifies a local review draft within one branch.
type DraftID int

// String returns the decimal command-line representation of the ID.
func (id DraftID) String() string {
	return strconv.Itoa(int(id))
}

// Draft is a local review comment waiting to be published.
type Draft struct {
	ID   DraftID // required
	Body string  // required

	// Anchor identifies the location of a root comment.
	// It is zero for a reply.
	Anchor Anchor

	// ReplyTo is the command-line ID of the replied-to thread.
	// It is empty for a root comment.
	ReplyTo string
}

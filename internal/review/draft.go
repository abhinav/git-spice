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
	id      DraftID
	body    string
	anchor  Anchor
	replyTo string
}

// NewCommentDraft returns a draft that starts a thread at anchor.
func NewCommentDraft(id DraftID, anchor Anchor, body string) Draft {
	return Draft{
		id:     id,
		body:   body,
		anchor: anchor,
	}
}

// NewReplyDraft returns a draft that replies to a review thread.
func NewReplyDraft(id DraftID, replyTo, body string) Draft {
	return Draft{
		id:      id,
		body:    body,
		replyTo: replyTo,
	}
}

// ID reports the branch-local draft identifier.
func (d Draft) ID() DraftID {
	return d.id
}

// Body reports the Markdown comment body.
func (d Draft) Body() string {
	return d.body
}

// Anchor reports where a root comment is attached.
// The second result is false for a reply.
func (d Draft) Anchor() (Anchor, bool) {
	if d.replyTo != "" {
		return Anchor{}, false
	}
	return d.anchor, true
}

// ReplyTo reports the command-line ID of the replied-to thread.
// The second result is false for a root comment.
func (d Draft) ReplyTo() (string, bool) {
	return d.replyTo, d.replyTo != ""
}

// WithID returns the draft with its branch-local identifier replaced.
func (d Draft) WithID(id DraftID) Draft {
	d.id = id
	return d
}

// WithBody returns the draft with its body replaced.
func (d Draft) WithBody(body string) Draft {
	d.body = body
	return d
}

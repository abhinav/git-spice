package scriptrun

import "encoding/json"

// ResolveResponse is the JSON document a script emits on stdout.
//
// All three script-driven features (commit message generation,
// restack auto-resolve, integration auto-resolve) speak the same
// protocol. Each feature reads the subset of fields that applies.
//
// See doc/src/guide/scripts.md for the user-facing contract.
type ResolveResponse struct {
	// Title is the first-line commit message or change-request title.
	// Read by message-generation features; ignored by auto-resolvers.
	Title string `json:"title,omitempty"`

	// Body is the multi-line body content for a commit or change
	// request. Read by message-generation features.
	Body string `json:"body,omitempty"`

	// Assumptions are notes the script wants surfaced in the gs log
	// so the user can see what choices the script made. All features
	// log these at info level.
	Assumptions []string `json:"assumptions,omitempty"`

	// Questions, when non-empty, drives the interactive Q&A loop.
	// git-spice prompts the user for each question and re-invokes the
	// script with the answers persisted in the resolution file.
	Questions []string `json:"questions,omitempty"`

	// UnresolvedFiles lists paths the script could not resolve.
	// Read by auto-resolve features only; treated as "not done" --
	// the run is not terminal until this slice is empty.
	UnresolvedFiles []string `json:"unresolved_files,omitempty"`
}

// ParseResponse unmarshals raw script output into a ResolveResponse.
// Returns an error wrapping ErrInvalidOutput if the output is not a
// valid JSON document.
func ParseResponse(stdout []byte) (*ResolveResponse, error) {
	var res ResolveResponse
	if len(stdout) == 0 {
		return nil, ErrEmptyOutput
	}
	if err := json.Unmarshal(stdout, &res); err != nil {
		return nil, &InvalidOutputError{Err: err, Output: stdout}
	}
	return &res, nil
}

// QAPair is a single question-answer record in a persistent
// resolution file. Resolution-file packages share this type so that
// answers gathered for one feature can be referenced from another (or
// at minimum so the on-disk schema is uniform).
type QAPair struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

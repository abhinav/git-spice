package bitbucket

// API request/response types for the Bitbucket REST API v2.0.

// apiCreatePRRequest is the request body for creating a pull request.
type apiCreatePRRequest struct {
	Title             string        `json:"title"`
	Description       string        `json:"description,omitempty"`
	Source            apiBranchRef  `json:"source"`
	Destination       apiBranchRef  `json:"destination"`
	Reviewers         []apiReviewer `json:"reviewers,omitempty"`
	CloseSourceBranch bool          `json:"close_source_branch,omitempty"`
	Draft             bool          `json:"draft,omitempty"`
}

// apiUpdatePRRequest is the request body for updating a pull request.
type apiUpdatePRRequest struct {
	Title       string        `json:"title,omitempty"`
	Description string        `json:"description,omitempty"`
	Destination *apiBranchRef `json:"destination,omitempty"`
	Reviewers   []apiReviewer `json:"reviewers,omitempty"`
	Draft       *bool         `json:"draft,omitempty"`
}

// apiBranchRef references a branch in a repository.
type apiBranchRef struct {
	Branch apiBranch  `json:"branch"`
	Commit *apiCommit `json:"commit,omitempty"`
}

// apiBranch represents a branch name.
type apiBranch struct {
	Name string `json:"name"`
}

// apiReviewer represents a reviewer on a pull request.
type apiReviewer struct {
	UUID string `json:"uuid"`
}

// apiPullRequest is the response for a pull request.
type apiPullRequest struct {
	ID          int64        `json:"id"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	State       string       `json:"state"`
	Draft       bool         `json:"draft"`
	Source      apiBranchRef `json:"source"`
	Destination apiBranchRef `json:"destination"`
	Author      apiUser      `json:"author"`
	Reviewers   []apiUser    `json:"reviewers"`
	Links       apiPRLinks   `json:"links"`
	MergeCommit *apiCommit   `json:"merge_commit,omitempty"`
}

// apiPRLinks contains links related to a pull request.
type apiPRLinks struct {
	HTML apiLink `json:"html"`
}

// apiLink is a hyperlink.
type apiLink struct {
	Href string `json:"href"`
}

// apiUser represents a Bitbucket user.
type apiUser struct {
	UUID        string `json:"uuid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AccountID   string `json:"account_id"`
	Nickname    string `json:"nickname"`
}

// apiCommit represents a commit.
type apiCommit struct {
	Hash string `json:"hash"`
}

// apiPRList is the paginated response for listing pull requests.
type apiPRList struct {
	Values []apiPullRequest `json:"values"`
	Next   string           `json:"next,omitempty"`
}

// apiComment represents a comment on a pull request.
type apiComment struct {
	ID      int64      `json:"id"`
	Content apiContent `json:"content"`
	User    apiUser    `json:"user"`
	Links   apiPRLinks `json:"links"`
}

// apiContent represents comment content.
type apiContent struct {
	Raw string `json:"raw"`
}

// apiCommentList is the paginated response for listing comments.
type apiCommentList struct {
	Values []apiComment `json:"values"`
	Next   string       `json:"next,omitempty"`
}

// apiCreateCommentRequest is the request body for creating a comment.
type apiCreateCommentRequest struct {
	Content apiContent `json:"content"`
}

// apiWorkspaceMember represents a member of a Bitbucket workspace.
type apiWorkspaceMember struct {
	User apiUser `json:"user"`
}

// apiWorkspaceMemberList is the paginated response for listing workspace members.
type apiWorkspaceMemberList struct {
	Values []apiWorkspaceMember `json:"values"`
	Next   string               `json:"next,omitempty"`
}

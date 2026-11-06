package github

// PullRequest is the GitHub projection used by change lookup operations.
type PullRequest struct {
	// ID is the pull request's GraphQL node ID.
	ID ID `json:"id"`

	// Number is the repository-local pull request number.
	Number int `json:"number"`

	// URL is GitHub's browser URL for the pull request.
	URL string `json:"url"`

	// Title is the pull request title.
	Title string `json:"title"`

	// State is the pull request lifecycle state.
	State PullRequestState `json:"state"`

	// HeadRefOID is the Git object ID at the head of the pull request.
	HeadRefOID string `json:"headRefOid"`

	// BaseRefName is the target branch name.
	BaseRefName string `json:"baseRefName"`

	// IsDraft reports whether the pull request is a draft.
	IsDraft bool `json:"isDraft"`

	// HeadRepository identifies the repository containing the head branch.
	HeadRepository struct {
		// Owner identifies the account that owns the head repository.
		Owner struct {
			// Login is the owner's GitHub login.
			Login string `json:"login"`
		} `json:"owner"`

		// Name is the head repository name.
		Name string `json:"name"`
	} `json:"headRepository"`

	// Labels contains at most the first 100 labels.
	Labels struct {
		// Nodes contains the labels returned by GitHub.
		Nodes []struct {
			// Name is the label name.
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`

	// ReviewRequests contains at most the first 100 review requests.
	ReviewRequests struct {
		// Nodes contains the review requests returned by GitHub.
		Nodes []struct {
			// RequestedReviewer identifies the requested user or team.
			RequestedReviewer struct {
				// Login is the requested reviewer's GitHub login.
				Login string `json:"login"`
			} `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`

	// Assignees contains at most the first 100 assignees.
	Assignees struct {
		// Nodes contains the assigned users returned by GitHub.
		Nodes []struct {
			// Login is the assignee's GitHub login.
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignees"`
}

var pullRequestFields = compactGraphQL(`
	id,number,url,title,state,headRefOid,baseRefName,isDraft,
	headRepository{owner{login},name},
	labels(first: 100){nodes{name}},
	reviewRequests(first: 100){
		nodes{requestedReviewer{... on Actor{login}}}
	},
	assignees(first: 100){nodes{login}}
`)

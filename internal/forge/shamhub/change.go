package shamhub

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
)

// ListChanges reports all changes known to the forge.
func (sh *ShamHub) ListChanges() ([]*Change, error) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	changes := make([]*Change, len(sh.changes))
	for i, c := range sh.changes {
		change, err := sh.toChange(c)
		if err != nil {
			return nil, err
		}

		changes[i] = change
	}

	return changes, nil
}

// ChangeID is a unique identifier for a change on a ShamHub server.
type ChangeID int

var _ forge.ChangeID = ChangeID(0)

func (id ChangeID) String() string { return fmt.Sprintf("#%d", id) }

// shamChangeState records the state of a Change.
type shamChangeState int

const (
	// shamChangeOpen specifies that a change is open
	// and may be merged.
	shamChangeOpen shamChangeState = iota

	// shamChangeClosed indicates that a change has been closed
	// without being merged.
	shamChangeClosed

	// shamChangeMerged indicates that a change has been merged.
	shamChangeMerged
)

// shamBranch is a branch in a ShamHub-tracked repository.
type shamBranch struct {
	// Owner of the repository.
	Owner string

	// Repo is the name of the repository
	// under the owner's namespace.
	Repo string

	// Name is the name of the branch.
	Name string
}

func (b *shamBranch) RepoID() repoID {
	return repoID{Owner: b.Owner, Name: b.Repo}
}

func (b *shamBranch) String() string {
	return fmt.Sprintf("%s/%s:%s", b.Owner, b.Repo, b.Name)
}

// shamChange is the internal representation of a [Change].
type shamChange struct {
	// State is the current state of the change.
	// It can be open, closed, or merged.
	State shamChangeState

	// Number is the numeric identifier of the change.
	// These increment monotonically.
	Number int

	// Draft indicates that the change is not yet ready to be reviewed.
	Draft bool

	Subject string
	Body    string

	// Base and Head branches for the change.
	// Head will merge into Base.
	Base, Head *shamBranch

	// Labels are the labels associated with the change.
	Labels []string

	// RequestedReviewers are the usernames of users
	// from whom reviews have been requested.
	RequestedReviewers []string

	// Assignees are users assigned to the change.
	Assignees []string
}

// Change is a change proposal against a repository.
type Change struct {
	// Number is the unique identifier of the change
	// under the Base repository.
	Number int `json:"number"`

	// URL is the URL to the change proposal on the ShamHub server.
	URL string `json:"html_url"`

	// Draft indicates that the change is not yet ready to be reviewed.
	Draft bool `json:"draft,omitempty"`

	// State is the current state of the change.
	// It may be "open" or "closed".
	State string `json:"state"`

	// Merged indicates that the change has been merged.
	Merged bool `json:"merged,omitempty"`

	// Historical note:
	// Merged is not just another State
	// because this was originally modeled after GitHub's V3 API.

	// Subject is the title of the change proposal.
	Subject string `json:"title"`

	// Body is the description of the change proposal.
	Body string `json:"body"`

	// Base is the branch into which the change will be merged.
	Base *ChangeBranch `json:"base"`

	// Head is the branch that contains the changes to be merged.
	// It is the source of the change proposal.
	Head *ChangeBranch `json:"head"`

	// Labels are the labels associated with the change.
	Labels []string `json:"labels,omitempty"`

	// RequestedReviewers are the usernames of users
	// from whom reviews have been requested.
	RequestedReviewers []string `json:"requested_reviewers,omitempty"`

	// Assignees are users assigned to the change.
	Assignees []string `json:"assignees,omitempty"`
}

// toChange converts an internal shamChange
// into a public Change.
func (sh *ShamHub) toChange(c shamChange) (*Change, error) {
	base, err := sh.toChangeBranch(c.Base)
	if err != nil {
		return nil, fmt.Errorf("base branch: %w", err)
	}

	// Determine head repository
	head, err := sh.toChangeBranch(c.Head)
	if err != nil {
		return nil, fmt.Errorf("head branch: %w", err)
	}

	requestedReviewers := slices.Clone(c.RequestedReviewers)
	slices.Sort(requestedReviewers)

	assignees := slices.Clone(c.Assignees)
	slices.Sort(assignees)

	change := &Change{
		Number:             c.Number,
		URL:                sh.changeURL(c.Base.Owner, c.Base.Repo, c.Number),
		Draft:              c.Draft,
		Subject:            c.Subject,
		Body:               c.Body,
		Base:               base,
		Head:               head,
		Labels:             c.Labels,
		RequestedReviewers: requestedReviewers,
		Assignees:          assignees,
	}
	switch c.State {
	case shamChangeOpen:
		change.State = "open"
	case shamChangeClosed:
		change.State = "closed"
	case shamChangeMerged:
		change.State = "closed"
		change.Merged = true
	default:
		return nil, fmt.Errorf("unknown change state: %d", c.State)
	}

	return change, nil
}

// ChangeBranch is a branch in a change proposal.
type ChangeBranch struct {
	// Repo is the repository in which the branch exists.
	Repo repoID `json:"repository"`

	// Name is the name of the branch.
	Name string `json:"ref"`

	// Hash is the SHA of the branch in the repository.
	Hash string `json:"sha"`
}

func (sh *ShamHub) toChangeBranch(b *shamBranch) (*ChangeBranch, error) {
	logw, flush := silog.Writer(sh.log, silog.LevelDebug)
	defer flush()

	cmd := exec.Command(sh.gitExe, "rev-parse", b.Name)
	cmd.Dir = sh.repoDir(b.Owner, b.Repo)
	cmd.Stderr = logw
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("get SHA for %v/%v:%v: %w", b.Owner, b.Repo, b.Name, err)
	}

	return &ChangeBranch{
		Repo: b.RepoID(),
		Name: b.Name,
		Hash: strings.TrimSpace(string(out)),
	}, nil
}

package shamhub

import (
	"fmt"
	"os/exec"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/log"
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

// shamChange is the internal representation of a [Change].
type shamChange struct {
	Owner string
	Repo  string

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

	// Name of the base branch.
	Base string

	// Name of the head branch.
	// This is the branch that contains the changes to be merged.
	Head string
}

// Change is a change proposal against a repository.
type Change struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`

	Draft  bool   `json:"draft,omitempty"`
	State  string `json:"state"`
	Merged bool   `json:"merged,omitempty"`

	Subject string `json:"title"`
	Body    string `json:"body"`

	Base *ChangeBranch `json:"base"`
	Head *ChangeBranch `json:"head"`
}

func (sh *ShamHub) toChange(c shamChange) (*Change, error) {
	base, err := sh.toChangeBranch(c.Owner, c.Repo, c.Base)
	if err != nil {
		return nil, fmt.Errorf("base branch: %w", err)
	}

	head, err := sh.toChangeBranch(c.Owner, c.Repo, c.Head)
	if err != nil {
		return nil, fmt.Errorf("head branch: %w", err)
	}

	change := &Change{
		Number:  c.Number,
		URL:     sh.changeURL(c.Owner, c.Repo, c.Number),
		Draft:   c.Draft,
		Subject: c.Subject,
		Body:    c.Body,
		Base:    base,
		Head:    head,
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
	Name string `json:"ref"`
	Hash string `json:"sha"`
}

func (sh *ShamHub) toChangeBranch(owner, repo, ref string) (*ChangeBranch, error) {
	logw, flush := log.Writer(sh.log, log.LevelDebug)
	defer flush()

	cmd := exec.Command(sh.gitExe, "rev-parse", ref)
	cmd.Dir = sh.repoDir(owner, repo)
	cmd.Stderr = logw
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("get SHA for %v/%v:%v: %w", owner, repo, ref, err)
	}

	return &ChangeBranch{
		Name: ref,
		Hash: strings.TrimSpace(string(out)),
	}, nil
}

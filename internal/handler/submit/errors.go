package submit

import (
	"fmt"

	"go.abhg.dev/gs/internal/git"
)

// RemoteAdvancedError indicates that the remote upstream branch gained
// commits on top of what git-spice last pushed, so a force-push would
// discard them.
//
// The user can bring those commits into their branch with 'gs branch sync'
// and submit again, or use --force to overwrite them.
type RemoteAdvancedError struct {
	// Branch is the local branch being submitted.
	Branch string

	// Remote is the name of the remote being pushed to.
	Remote string

	// UpstreamBranch is the name of the branch on the remote.
	UpstreamBranch string

	// Commits are the commits present on the remote
	// that are not in the local branch, newest first.
	Commits []git.CommitDetail
}

func (e *RemoteAdvancedError) Error() string {
	return fmt.Sprintf(
		"%v/%v has %d commit(s) not in branch %v",
		e.Remote, e.UpstreamBranch, len(e.Commits), e.Branch,
	)
}

// RemoteDivergedError indicates that the remote upstream branch was rewritten
// away from what git-spice last pushed, so we cannot identify what a
// force-push would discard.
type RemoteDivergedError struct {
	// Branch is the local branch being submitted.
	Branch string

	// Remote is the name of the remote being pushed to.
	Remote string

	// UpstreamBranch is the name of the branch on the remote.
	UpstreamBranch string

	// LastPushed is the commit git-spice last pushed to the upstream branch.
	LastPushed git.Hash

	// RemoteHash is the upstream branch's current head.
	RemoteHash git.Hash
}

func (e *RemoteDivergedError) Error() string {
	return fmt.Sprintf(
		"%v/%v was rewritten away from the last pushed commit %v",
		e.Remote, e.UpstreamBranch, e.LastPushed.Short(),
	)
}

// RemoteNoBaselineError indicates that git-spice has no record of what it last
// pushed to the upstream branch, and the remote differs from the local branch,
// so it cannot prove that a force-push is safe.
type RemoteNoBaselineError struct {
	// Branch is the local branch being submitted.
	Branch string

	// Remote is the name of the remote being pushed to.
	Remote string

	// UpstreamBranch is the name of the branch on the remote.
	UpstreamBranch string

	// RemoteHash is the upstream branch's current head.
	RemoteHash git.Hash

	// LocalHash is the local branch's head.
	LocalHash git.Hash
}

func (e *RemoteNoBaselineError) Error() string {
	return fmt.Sprintf(
		"no record of the last commit pushed to %v/%v",
		e.Remote, e.UpstreamBranch,
	)
}

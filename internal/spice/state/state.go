package state

import (
	"errors"
	"path"
)

const (
	_repoJSON           = "repo"
	_branchesDir        = "branches"
	_rebaseContinueJSON = "rebase-continue"
)

type repoInfo struct {
	Trunk  string `json:"trunk"`
	Remote string `json:"remote"`
}

func (i *repoInfo) Validate() error {
	if i.Trunk == "" {
		return errors.New("trunk branch name is empty")
	}
	return nil
}

type rebaseContinueState struct {
	Continuations []rebaseContinuation `json:"continuations"`
}

type rebaseContinuation struct {
	// Command is the gs command that will be run.
	Command []string `json:"command"`

	// Branch on which the command must be run.
	Branch string `json:"branch"`
}

type branchStateBase struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type branchGitHubState struct {
	PR int `json:"pr,omitempty"`
}

type branchUpstreamState struct {
	Branch string `json:"branch,omitempty"`
}

type branchState struct {
	Base     branchStateBase      `json:"base"`
	Upstream *branchUpstreamState `json:"upstream,omitempty"`
	GitHub   *branchGitHubState   `json:"github,omitempty"`
}

// branchJSON returns the path to the JSON file for the given branch
// relative to the store's root.
func (s *Store) branchJSON(name string) string {
	return path.Join(_branchesDir, name)
}

package state

import "path"

const (
	_repoJSON    = "repo"
	_branchesDir = "branches"
)

type repoInfo struct {
	Trunk  string      `json:"trunk"`
	GitHub *gitHubInfo `json:"github"`
}

type gitHubInfo struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

type branchStateBase struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type branchState struct {
	Base branchStateBase `json:"base"`
	PR   int             `json:"pr,omitempty"`
}

// branchJSON returns the path to the JSON file for the given branch
// relative to the store's root.
func (s *Store) branchJSON(name string) string {
	return path.Join(_branchesDir, name)
}

package state

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/spice/state/storage"
)

const _repoJSON = "repo"

// Remote identifies the Git remotes used by git-spice.
type Remote struct {
	// Upstream is the remote that hosts trunk and change requests.
	Upstream string

	// Push is the remote that receives submitted branch pushes.
	Push string
}

// ForkMode reports whether the repository uses different remotes
// for upstream operations and branch pushes.
func (r Remote) ForkMode() bool {
	return r.Upstream != "" && r.Push != "" && r.Upstream != r.Push
}

type remoteInfo struct {
	Upstream string `json:"upstream,omitempty"`
	Push     string `json:"push,omitempty"`
}

func newRemoteInfo(remote Remote) remoteInfo {
	return remoteInfo(remote)
}

type repoInfo struct {
	Trunk   string      `json:"trunk"`
	Remote  string      `json:"remote,omitempty"`
	Remotes *remoteInfo `json:"remotes,omitempty"`
}

func newRepoInfo(trunk string, remote Remote) repoInfo {
	info := repoInfo{
		Trunk: trunk,
	}

	switch {
	case remote == (Remote{}):
		// No remote configured.
	case remote.ForkMode():
		// Older binaries must not guess at fork-mode semantics.
		// The version file gates this v2-only field.
		info.Remote = remote.Upstream
		remotes := newRemoteInfo(remote)
		info.Remotes = &remotes
	default:
		info.Remote = cmp.Or(remote.Upstream, remote.Push)
	}

	return info
}

func (i *repoInfo) stateRemote() Remote {
	if r := i.Remotes; r != nil {
		return Remote{
			Upstream: r.Upstream,
			Push:     r.Push,
		}
	}
	if i.Remote == "" {
		return Remote{}
	}
	return Remote{
		Upstream: i.Remote,
		Push:     i.Remote,
	}
}

func (i *repoInfo) UnmarshalJSON(data []byte) error {
	var raw struct {
		Trunk   string          `json:"trunk"`
		Remote  json.RawMessage `json:"remote"`
		Remotes *remoteInfo     `json:"remotes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	i.Trunk = raw.Trunk
	i.Remotes = raw.Remotes
	if len(raw.Remote) == 0 || string(raw.Remote) == "null" {
		return nil
	}

	var legacy string
	if err := json.Unmarshal(raw.Remote, &legacy); err == nil {
		i.Remote = legacy
		return nil
	}

	var previous remoteInfo
	if err := json.Unmarshal(raw.Remote, &previous); err != nil {
		return fmt.Errorf("unmarshal remote: %w", err)
	}
	if previous != (remoteInfo{}) && i.Remotes == nil {
		i.Remotes = &previous
	}
	return nil
}

func (i *repoInfo) Validate() error {
	if i.Trunk == "" {
		return errors.New("trunk branch name is empty")
	}
	return nil
}

// Trunk reports the trunk branch configured for the repository.
func (s *Store) Trunk() string {
	return s.trunk
}

// Remote returns the remotes configured for the repository.
// Returns [ErrNotExist] if no remote is configured.
func (s *Store) Remote() (Remote, error) {
	if s.remote == (Remote{}) {
		return Remote{}, ErrNotExist
	}

	return s.remote, nil
}

// SetRemote changes the remotes configured for the repository.
func (s *Store) SetRemote(ctx context.Context, remote Remote) error {
	var info repoInfo
	if err := s.db.Get(ctx, _repoJSON, &info); err != nil {
		return fmt.Errorf("get repo info: %w", err)
	}
	info = newRepoInfo(info.Trunk, remote)

	if err := info.Validate(); err != nil {
		// Technically impossible if state was already validated
		// but worth checking to be sure.
		return fmt.Errorf("would corrupt state: %w", err)
	}

	version := storageVersionForRemote(remote)
	if err := s.db.Update(ctx, storage.UpdateRequest{
		Sets: []storage.SetRequest{
			{
				Key:   _repoJSON,
				Value: info,
			},
			{
				Key:   _versionFile,
				Value: version,
			},
		},
		Message: fmt.Sprintf("set remote: %v", remote),
	}); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	s.remote = remote

	return nil
}

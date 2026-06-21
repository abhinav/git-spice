package state

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
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
	Trunk       string           `json:"trunk"`
	Remote      string           `json:"remote,omitempty"`
	Remotes     *remoteInfo      `json:"remotes,omitempty"`
	Integration *integrationInfo `json:"integration,omitempty"`
}

// integrationInfo persists the configured integration branch.
//
// An integration branch is a separate, repo-scoped singleton:
// it combines the tips of multiple tracked branches by sequentially
// merging them onto trunk. It is not a tracked stack branch.
type integrationInfo struct {
	Name           string               `json:"name"`
	UpstreamBranch string               `json:"upstreamBranch,omitempty"`
	LastPushedHash string               `json:"lastPushedHash,omitempty"`
	Tips           []integrationTipInfo `json:"tips,omitempty"`
}

// integrationTipInfo identifies a branch whose tip composes the integration
// branch, along with the hash recorded at the last successful rebuild.
type integrationTipInfo struct {
	Name string `json:"name"`
	Hash string `json:"hash,omitempty"`
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
		Trunk       string           `json:"trunk"`
		Remote      json.RawMessage  `json:"remote"`
		Remotes     *remoteInfo      `json:"remotes"`
		Integration *integrationInfo `json:"integration"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	i.Trunk = raw.Trunk
	i.Remotes = raw.Remotes
	i.Integration = raw.Integration
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
	if i.Integration != nil {
		if err := i.Integration.validate(i.Trunk); err != nil {
			return err
		}
	}
	return nil
}

func (n *integrationInfo) validate(trunk string) error {
	if n.Name == "" {
		return errors.New("integration branch name is empty")
	}
	if n.Name == trunk {
		return errors.New("integration branch name must not equal trunk")
	}
	for _, tip := range n.Tips {
		if tip.Name == "" {
			return errors.New("integration tip name is empty")
		}
		if tip.Name == trunk {
			return errors.New("integration tip must not equal trunk")
		}
		if tip.Name == n.Name {
			return errors.New("integration tip must not equal integration branch name")
		}
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
	integration := info.Integration
	info = newRepoInfo(info.Trunk, remote)
	info.Integration = integration

	if err := info.Validate(); err != nil {
		// Technically impossible if state was already validated
		// but worth checking to be sure.
		return fmt.Errorf("would corrupt state: %w", err)
	}

	if err := s.db.Update(ctx, storage.UpdateRequest{
		Sets: []storage.SetRequest{
			{
				Key:   _repoJSON,
				Value: info,
			},
			{
				Key:   _versionFile,
				Value: storageVersion(info),
			},
		},
		Message: fmt.Sprintf("set remote: %v", remote),
	}); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	s.remote = remote

	return nil
}

// IntegrationInfo describes the configured integration branch.
//
// An integration branch is a repo-scoped singleton that is rebuilt by
// sequentially merging configured tip branches onto trunk. It is
// distinct from tracked stack branches.
type IntegrationInfo struct {
	// Name is the local branch name of the integration branch.
	Name string

	// UpstreamBranch is the remote branch name when pushed.
	// Defaults to Name if empty.
	UpstreamBranch string

	// LastPushedHash is the integration branch hash at the last successful
	// push. Used for --force-with-lease on subsequent submits. An empty
	// value indicates the branch has never been submitted.
	LastPushedHash git.Hash

	// Tips lists the branches whose tips compose the integration branch.
	Tips []IntegrationTip
}

// IntegrationTip identifies a branch whose tip is merged into the
// integration branch, along with the hash recorded at the last
// successful rebuild.
type IntegrationTip struct {
	// Name is the local branch name of the tip.
	Name string

	// Hash is the tip's hash at the last successful integration rebuild.
	// Compared against the current hash to detect drift.
	Hash git.Hash
}

// Integration returns the configured integration branch, or
// [ErrNotExist] if none is configured.
func (s *Store) Integration(ctx context.Context) (*IntegrationInfo, error) {
	var info repoInfo
	if err := s.db.Get(ctx, _repoJSON, &info); err != nil {
		return nil, fmt.Errorf("get repo info: %w", err)
	}
	if info.Integration == nil {
		return nil, ErrNotExist
	}
	return integrationInfoToPublic(info.Integration), nil
}

// SetIntegration writes the integration branch configuration.
// Pass nil to clear the configuration.
func (s *Store) SetIntegration(ctx context.Context, integration *IntegrationInfo) error {
	var info repoInfo
	if err := s.db.Get(ctx, _repoJSON, &info); err != nil {
		return fmt.Errorf("get repo info: %w", err)
	}

	if integration == nil {
		info.Integration = nil
	} else {
		info.Integration = integrationInfoFromPublic(integration)
	}

	if err := info.Validate(); err != nil {
		return fmt.Errorf("would corrupt state: %w", err)
	}

	msg := "set integration"
	if integration == nil {
		msg = "clear integration"
	}

	if err := s.db.Update(ctx, storage.UpdateRequest{
		Sets: []storage.SetRequest{
			{
				Key:   _repoJSON,
				Value: info,
			},
			{
				Key:   _versionFile,
				Value: storageVersion(info),
			},
		},
		Message: msg,
	}); err != nil {
		return fmt.Errorf("update: %w", err)
	}

	return nil
}

func integrationInfoToPublic(in *integrationInfo) *IntegrationInfo {
	tips := make([]IntegrationTip, len(in.Tips))
	for i, t := range in.Tips {
		tips[i] = IntegrationTip{Name: t.Name, Hash: git.Hash(t.Hash)}
	}
	return &IntegrationInfo{
		Name:           in.Name,
		UpstreamBranch: in.UpstreamBranch,
		LastPushedHash: git.Hash(in.LastPushedHash),
		Tips:           tips,
	}
}

func integrationInfoFromPublic(in *IntegrationInfo) *integrationInfo {
	var tips []integrationTipInfo
	if len(in.Tips) > 0 {
		tips = make([]integrationTipInfo, len(in.Tips))
		for i, t := range in.Tips {
			tips[i] = integrationTipInfo{Name: t.Name, Hash: t.Hash.String()}
		}
	}
	return &integrationInfo{
		Name:           in.Name,
		UpstreamBranch: in.UpstreamBranch,
		LastPushedHash: in.LastPushedHash.String(),
		Tips:           tips,
	}
}

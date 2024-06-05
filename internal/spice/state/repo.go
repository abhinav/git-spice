package state

import (
	"context"
	"errors"
	"fmt"
)

const _repoJSON = "repo"

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

// Trunk reports the trunk branch configured for the repository.
func (s *Store) Trunk() string {
	return s.trunk
}

// Remote returns the remote configured for the repository.
// Returns [ErrNotExist] if no remote is configured.
func (s *Store) Remote() (string, error) {
	if s.remote == "" {
		return "", ErrNotExist
	}

	return s.remote, nil
}

// SetRemote changes teh remote name configured for the repository.
func (s *Store) SetRemote(ctx context.Context, remote string) error {
	var info repoInfo
	if err := s.b.Get(ctx, _repoJSON, &info); err != nil {
		return fmt.Errorf("get repo info: %w", err)
	}
	info.Remote = remote

	if err := info.Validate(); err != nil {
		// Technically impossible if state was already validated
		// but worth checking to be sure.
		return fmt.Errorf("would corrupt state: %w", err)
	}

	err := s.b.Update(ctx, updateRequest{
		Sets: []setRequest{
			{
				Key: _repoJSON,
				Val: info,
			},
		},
		Msg: fmt.Sprintf("set remote: %v", remote),
	})
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	return nil
}

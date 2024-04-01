// Package state defines and stores the state for gs.
package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path"
	"strings"

	"go.abhg.dev/gs/internal/git"
)

const (
	_dataRef     = "refs/gs/data"
	_repoJSON    = "repo.json"
	_branchesDir = "branches"

	_authorName  = "gs"
	_authorEmail = "gs@localhost"
)

var ErrNotExist = os.ErrNotExist

type StateHash git.Hash

// GitRepository is the subset of the git.Repository API used by the state package.
type GitRepository interface {
	PeelToCommit(ctx context.Context, ref string) (git.Hash, error)
	PeelToTree(ctx context.Context, ref string) (git.Hash, error)
	BlobAt(ctx context.Context, treeish, path string) (git.Hash, error)
	TreeAt(ctx context.Context, commitish, path string) (git.Hash, error)

	ReadObject(ctx context.Context, typ git.Type, hash git.Hash, dst io.Writer) error
	WriteObject(ctx context.Context, typ git.Type, src io.Reader) (git.Hash, error)

	MakeTree(ctx context.Context, ents iter.Seq[git.TreeEntry]) (git.Hash, error)
	ListTree(ctx context.Context, tree git.Hash, opts git.ListTreeOptions) (iter.Seq2[git.TreeEntry, error], error)
	CommitTree(ctx context.Context, req git.CommitTreeRequest) (git.Hash, error)
	UpdateTree(ctx context.Context, req git.UpdateTreeRequest) (git.Hash, error)

	SetRef(ctx context.Context, req git.SetRefRequest) error
}

var _ GitRepository = (*git.Repository)(nil)

// Store implements storage for gs state inside a Git repository.
type Store struct {
	repo  GitRepository
	trunk string
}

func (s *Store) Trunk() string {
	return s.trunk
}

type InitStoreRequest struct {
	// Repository is the Git repository in which to store the state.
	Repository GitRepository

	// Trunk is the name of the trunk branch,
	// e.g. "main" or "master".
	Trunk string
}

type repoState struct {
	Trunk string `json:"trunk"`
}

func InitStore(ctx context.Context, req InitStoreRequest) (*Store, error) {
	repo := req.Repository
	if req.Trunk == "" {
		return nil, errors.New("trunk branch name is required")
	}

	if _, err := repo.PeelToCommit(ctx, _dataRef); err == nil {
		return nil, errors.New("store already initialized")
	}

	data, err := json.MarshalIndent(repoState{
		Trunk: req.Trunk,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal state: %w", err)
	}

	blobHash, err := repo.WriteObject(ctx, git.BlobType, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("write state blob: %w", err)
	}

	treeHash, err := repo.MakeTree(ctx, func(yield func(git.TreeEntry) bool) {
		yield(git.TreeEntry{
			Name: _repoJSON,
			Type: git.BlobType,
			Mode: git.RegularMode,
			Hash: blobHash,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("make tree: %w", err)
	}

	commitHash, err := repo.CommitTree(ctx, git.CommitTreeRequest{
		Tree:    treeHash,
		Message: "gs: initialize store",
		Author:  &git.Signature{Name: _authorName, Email: _authorEmail},
	})
	if err != nil {
		return nil, fmt.Errorf("commit tree: %w", err)
	}

	setReq := git.SetRefRequest{
		Ref:     _dataRef,
		Hash:    commitHash,
		OldHash: git.ZeroHash,
	}
	if err := repo.SetRef(ctx, setReq); err != nil {
		// TODO: if ref set failed, another process may have initialized the store.
		// We should check for this and return an error if so.
		return nil, fmt.Errorf("set data ref: %w", err)
	}

	return &Store{
		repo:  repo,
		trunk: req.Trunk,
	}, nil
}

var ErrUninitialized = errors.New("store not initialized")

// OpenStore opens the Store for the given Git repository.
// The store will be created if it does not exist.
func OpenStore(ctx context.Context, repo GitRepository) (*Store, error) {
	commitHash, err := repo.PeelToCommit(ctx, _dataRef)
	if err != nil {
		if errors.Is(err, git.ErrNotExist) {
			return nil, ErrUninitialized
		}
		return nil, fmt.Errorf("get data commit: %w", err)
	}

	blobHash, err := repo.BlobAt(ctx, commitHash.String(), _repoJSON)
	if err != nil {
		return nil, fmt.Errorf("get repo blob hash: %w", err)
	}

	var buf bytes.Buffer
	if err := repo.ReadObject(ctx, git.BlobType, blobHash, &buf); err != nil {
		return nil, fmt.Errorf("read repo blob: %w", err)
	}

	var state repoState
	if err := json.Unmarshal(buf.Bytes(), &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}

	return &Store{
		repo:  repo,
		trunk: state.Trunk,
	}, nil
}

func (s *Store) branchJSON(name string) string {
	return path.Join(_branchesDir, name+".json")
}

type GetBranchResponse struct {
	Base string
	PR   int
}

// GetBranch returns information about a branch tracked by gs.
// If the branch is not found, [ErrNotExist] will be returned.
func (s *Store) GetBranch(ctx context.Context, name string) (GetBranchResponse, error) {
	blobHash, err := s.repo.BlobAt(ctx, _dataRef, s.branchJSON(name))
	if err != nil {
		if errors.Is(err, git.ErrNotExist) {
			return GetBranchResponse{}, ErrNotExist
		}
		return GetBranchResponse{}, fmt.Errorf("get blob hash: %w", err)
	}

	var buf bytes.Buffer
	if err := s.repo.ReadObject(ctx, git.BlobType, blobHash, &buf); err != nil {
		return GetBranchResponse{}, fmt.Errorf("read blob: %w", err)
	}

	// TODO: layer on top of git.Repository to abstract JSON storage.

	var state stateBranch
	if err := json.Unmarshal(buf.Bytes(), &state); err != nil {
		return GetBranchResponse{}, fmt.Errorf("unmarshal state: %w", err)
	}

	return GetBranchResponse(state), nil
}

// SetBranchRequest is a request to update information about a branch
// tracked by gs.
type SetBranchRequest struct {
	// Name is the name of the branch.
	Name string

	// Base is the parent branch of the branch.
	// This is immediately upstack from the branch,
	// and is the branch into which a PR would be merged.
	Base string

	// PR is the number of the pull request associated with the branch.
	// Zero if the branch is not associated with a PR yet.
	PR int
}

type stateBranch struct {
	Base string `json:"base"`
	PR   int    `json:"pr,omitempty"`
}

// SetBranch updates information about a branch tracked by gs.
func (s *Store) SetBranch(ctx context.Context, req SetBranchRequest) error {
	data, err := json.MarshalIndent(stateBranch{
		Base: req.Base,
		PR:   req.PR,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	branchBlob, err := s.repo.WriteObject(ctx, git.BlobType, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("write state blob: %w", err)
	}

	commitHash, err := s.repo.PeelToCommit(ctx, _dataRef)
	if err != nil {
		return fmt.Errorf("gs not initialized: %w", err)
	}

	treeHash, err := s.repo.PeelToTree(ctx, commitHash.String())
	if err != nil {
		return fmt.Errorf("get tree hash: %w", err)
	}

	newTreeHash, err := s.repo.UpdateTree(ctx, git.UpdateTreeRequest{
		Tree: treeHash,
		Writes: func(yield func(git.BlobInfo) bool) {
			yield(git.BlobInfo{
				Path: s.branchJSON(req.Name),
				Hash: branchBlob,
			})
		},
	})
	if err != nil {
		return fmt.Errorf("update tree: %w", err)
	}

	newCommitHash, err := s.repo.CommitTree(ctx, git.CommitTreeRequest{
		Tree:    newTreeHash,
		Message: fmt.Sprintf("gs: update branch %q", req.Name),
		Parents: []git.Hash{commitHash},
		Author:  &git.Signature{Name: _authorName, Email: _authorEmail},
	})
	if err != nil {
		return fmt.Errorf("commit tree: %w", err)
	}

	setReq := git.SetRefRequest{
		Ref:     _dataRef,
		Hash:    newCommitHash,
		OldHash: commitHash,
	}
	// TODO: handle ref moved becauase another branch was updated concurrently
	if err := s.repo.SetRef(ctx, setReq); err != nil {
		return fmt.Errorf("set branches ref: %w", err)
	}

	return nil
}

func (s *Store) ForgetBranch(ctx context.Context, name string) error {
	commitHash, err := s.repo.PeelToCommit(ctx, _dataRef)
	if err != nil {
		return fmt.Errorf("gs not initialized: %w", err)
	}

	treeHash, err := s.repo.PeelToTree(ctx, commitHash.String())
	if err != nil {
		return fmt.Errorf("get tree hash: %w", err)
	}

	newTreeHash, err := s.repo.UpdateTree(ctx, git.UpdateTreeRequest{
		Tree: treeHash,
		Deletes: func(yield func(string) bool) {
			yield(s.branchJSON(name))
		},
	})
	if err != nil {
		return fmt.Errorf("update tree: %w", err)
	}

	newCommitHash, err := s.repo.CommitTree(ctx, git.CommitTreeRequest{
		Tree:    newTreeHash,
		Message: fmt.Sprintf("gs: forget branch %q", name),
		Parents: []git.Hash{commitHash},
	})
	if err != nil {
		return fmt.Errorf("commit tree: %w", err)
	}

	setReq := git.SetRefRequest{
		Ref:     _dataRef,
		Hash:    newCommitHash,
		OldHash: commitHash,
	}
	if err := s.repo.SetRef(ctx, setReq); err != nil {
		return fmt.Errorf("set branches ref: %w", err)
	}

	return nil
}

// UpstackDirect lists branches that are immediately upstack from the given branch.
func (s *Store) UpstackDirect(ctx context.Context, parent string) ([]string, error) {
	treeHash, err := s.repo.TreeAt(ctx, _dataRef, _branchesDir)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("get tree hash: %w", err)
	}

	ents, err := s.repo.ListTree(ctx, treeHash, git.ListTreeOptions{
		Recurse: true,
	})
	if err != nil {
		return nil, fmt.Errorf("list tree: %w", err)
	}

	var (
		children []string
		buff     bytes.Buffer
	)
	for ent, err := range ents {
		if err != nil {
			return nil, fmt.Errorf("list tree: %w", err)
		}

		buff.Reset()
		if err := s.repo.ReadObject(ctx, git.BlobType, ent.Hash, &buff); err != nil {
			return nil, fmt.Errorf("read branch %q: %w", ent.Name, err)
		}

		var branch stateBranch
		if err := json.Unmarshal(buff.Bytes(), &branch); err != nil {
			return nil, fmt.Errorf("unmarshal branch %q: %w", ent.Name, err)
		}

		branchName := strings.TrimSuffix(ent.Name, ".json")
		if branch.Base == parent {
			children = append(children, branchName)
		}
	}

	return children, nil
}

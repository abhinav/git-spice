package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
)

const (
	_dataRef     = "refs/gs/data"
	_authorName  = "git-spice"
	_authorEmail = "git-spice@localhost"
)

// GitRepository is the subset of the git.Repository API used by the state package.
type GitRepository interface {
	PeelToCommit(ctx context.Context, ref string) (git.Hash, error)
	PeelToTree(ctx context.Context, ref string) (git.Hash, error)
	BlobAt(ctx context.Context, treeish, path string) (git.Hash, error)
	TreeAt(ctx context.Context, commitish, path string) (git.Hash, error)

	ReadObject(ctx context.Context, typ git.Type, hash git.Hash, dst io.Writer) error
	WriteObject(ctx context.Context, typ git.Type, src io.Reader) (git.Hash, error)

	ListTree(ctx context.Context, tree git.Hash, opts git.ListTreeOptions) ([]git.TreeEntry, error)
	CommitTree(ctx context.Context, req git.CommitTreeRequest) (git.Hash, error)
	UpdateTree(ctx context.Context, req git.UpdateTreeRequest) (git.Hash, error)
	MakeTree(ctx context.Context, ents []git.TreeEntry) (git.Hash, error)

	SetRef(ctx context.Context, req git.SetRefRequest) error
}

var _ GitRepository = (*git.Repository)(nil)

// storageBackend abstracts away the JSON value storage for the state store.
// There's only one implementation in practice (gitStorageBackend).
type storageBackend interface {
	Get(ctx context.Context, key string, v interface{}) error
	Clear(ctx context.Context, msg string) error
	Update(ctx context.Context, req updateRequest) error
	Keys(ctx context.Context, dir string) ([]string, error)
}

type gitStorageBackend struct {
	repo GitRepository
	ref  string
	sig  git.Signature
	log  *log.Logger
}

var _ storageBackend = (*gitStorageBackend)(nil)

func newGitStorageBackend(repo GitRepository, log *log.Logger) *gitStorageBackend {
	return &gitStorageBackend{
		repo: repo,
		ref:  _dataRef,
		sig: git.Signature{
			Name:  _authorName,
			Email: _authorEmail,
		},
		log: log,
	}
}

func (g *gitStorageBackend) Keys(ctx context.Context, dir string) ([]string, error) {
	var (
		treeHash git.Hash
		err      error
	)
	if dir == "" {
		treeHash, err = g.repo.PeelToTree(ctx, g.ref)
	} else {
		treeHash, err = g.repo.TreeAt(ctx, g.ref, dir)
	}
	if err != nil {
		if errors.Is(err, git.ErrNotExist) {
			return nil, nil // no keys
		}
		return nil, fmt.Errorf("get tree hash: %w", err)
	}

	entries, err := g.repo.ListTree(ctx, treeHash, git.ListTreeOptions{
		Recurse: true,
	})
	if err != nil {
		return nil, fmt.Errorf("list tree: %w", err)
	}

	var keys []string
	for _, ent := range entries {
		if ent.Type != git.BlobType {
			continue
		}

		keys = append(keys, ent.Name)
	}

	return keys, nil
}

func (g *gitStorageBackend) Get(ctx context.Context, key string, v interface{}) error {
	blobHash, err := g.repo.BlobAt(ctx, g.ref, key)
	if err != nil {
		return ErrNotExist
	}

	var buf bytes.Buffer
	if err := g.repo.ReadObject(ctx, git.BlobType, blobHash, &buf); err != nil {
		return fmt.Errorf("read object: %w", err)
	}

	if err := json.NewDecoder(&buf).Decode(v); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}

	return nil
}

func (g *gitStorageBackend) Clear(ctx context.Context, msg string) error {
	prevCommit, err := g.repo.PeelToCommit(ctx, g.ref)
	if err != nil {
		prevCommit = "" // not initialized
	}

	tree, err := g.repo.MakeTree(ctx, nil /* empty tree */)
	if err != nil {
		return fmt.Errorf("make tree: %w", err)
	}

	commitReq := git.CommitTreeRequest{
		Tree:    tree,
		Message: msg,
		Author:  &g.sig,
	}
	if prevCommit != "" {
		commitReq.Parents = []git.Hash{prevCommit}
	}
	newCommit, err := g.repo.CommitTree(ctx, commitReq)
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	if err := g.repo.SetRef(ctx, git.SetRefRequest{
		Ref:     g.ref,
		Hash:    newCommit,
		OldHash: prevCommit,
	}); err != nil {
		return fmt.Errorf("update ref: %w", err)
	}

	return nil
}

type setRequest struct {
	Key string
	Val interface{}
}

type updateRequest struct {
	Sets []setRequest // TODO: iterators?
	Dels []string
	Msg  string
}

func (g *gitStorageBackend) Update(ctx context.Context, req updateRequest) error {
	setBlobs := make([]git.Hash, len(req.Sets))
	for i, set := range req.Sets {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(set.Val); err != nil {
			return fmt.Errorf("encode JSON: %w", err)
		}

		blobHash, err := g.repo.WriteObject(ctx, git.BlobType, &buf)
		if err != nil {
			return fmt.Errorf("write object: %w", err)
		}

		setBlobs[i] = blobHash
	}

	var updateErr error
	for range 5 {
		var prevTree git.Hash
		prevCommit, err := g.repo.PeelToCommit(ctx, g.ref)
		if err != nil {
			prevCommit = ""
			prevTree = ""
		} else {
			prevTree, err = g.repo.PeelToTree(ctx, prevCommit.String())
			if err != nil {
				return fmt.Errorf("get tree for %v: %w", prevCommit, err)
			}
		}

		writes := make([]git.BlobInfo, len(req.Sets))
		for i, req := range req.Sets {
			writes[i] = git.BlobInfo{
				Mode: git.RegularMode,
				Path: req.Key,
				Hash: setBlobs[i],
			}
		}

		newTree, err := g.repo.UpdateTree(ctx, git.UpdateTreeRequest{
			Tree:    prevTree,
			Writes:  writes,
			Deletes: req.Dels,
		})
		if err != nil {
			return fmt.Errorf("update tree: %w", err)
		}

		commitReq := git.CommitTreeRequest{
			Tree:    newTree,
			Message: req.Msg,
			Author:  &g.sig,
		}
		if prevCommit != "" {
			commitReq.Parents = []git.Hash{prevCommit}
		}
		newCommit, err := g.repo.CommitTree(ctx, commitReq)
		if err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		if err := g.repo.SetRef(ctx, git.SetRefRequest{
			Ref:     g.ref,
			Hash:    newCommit,
			OldHash: prevCommit,
		}); err != nil {
			updateErr = err
			g.log.Warn("could not update ref: retrying", "err", err)
			continue
		}

		return nil
	}

	return fmt.Errorf("set ref: %w", updateErr)
}

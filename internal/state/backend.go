package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
)

const (
	_dataRef     = "refs/gs/data"
	_authorName  = "gs"
	_authorEmail = "gs@localhost"
)

// GitRepository is the subset of the git.Repository API used by the state package.
type GitRepository interface {
	PeelToCommit(ctx context.Context, ref string) (git.Hash, error)
	PeelToTree(ctx context.Context, ref string) (git.Hash, error)
	BlobAt(ctx context.Context, treeish, path string) (git.Hash, error)
	TreeAt(ctx context.Context, commitish, path string) (git.Hash, error)

	ReadObject(ctx context.Context, typ git.Type, hash git.Hash, dst io.Writer) error
	WriteObject(ctx context.Context, typ git.Type, src io.Reader) (git.Hash, error)

	ListTree(ctx context.Context, tree git.Hash, opts git.ListTreeOptions) (iter.Seq2[git.TreeEntry, error], error)
	CommitTree(ctx context.Context, req git.CommitTreeRequest) (git.Hash, error)
	UpdateTree(ctx context.Context, req git.UpdateTreeRequest) (git.Hash, error)
	MakeTree(ctx context.Context, ents iter.Seq[git.TreeEntry]) (git.Hash, error)

	SetRef(ctx context.Context, req git.SetRefRequest) error
}

var _ GitRepository = (*git.Repository)(nil)

type PutRequest struct {
	Key   string
	Value interface{}
}

type storageBackend interface {
	Put(ctx context.Context, key string, v interface{}, msg string) error
	BulkPut(ctx context.Context, reqs []PutRequest, msg string) error

	Get(ctx context.Context, key string, v interface{}) error
	Del(ctx context.Context, key string, msg string) error
	Clear(ctx context.Context, msg string) error
	Keys(ctx context.Context, dir string) (iter.Seq[string], error)
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

func (g *gitStorageBackend) Keys(ctx context.Context, dir string) (iter.Seq[string], error) {
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
			return func(func(string) bool) {}, nil
		}
		return nil, fmt.Errorf("get tree hash: %w", err)
	}

	entries, err := g.repo.ListTree(ctx, treeHash, git.ListTreeOptions{
		Recurse: true,
	})
	if err != nil {
		return nil, fmt.Errorf("list tree: %w", err)
	}
	return func(yield func(string) bool) {
		for ent, err := range entries {
			if err != nil {
				g.log.Warn("error encountered while reading tree entries",
					"err", err,
					"dir", dir,
					"tree", treeHash)
				break
			}

			if ent.Type != git.BlobType {
				continue
			}

			if !yield(ent.Name) {
				break
			}
		}
	}, nil
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

	tree, err := g.repo.MakeTree(ctx, func(yield func(git.TreeEntry) bool) {
		// empty
	})
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

func (g *gitStorageBackend) Put(ctx context.Context, key string, v interface{}, msg string) error {
	return g.BulkPut(ctx, []PutRequest{{Key: key, Value: v}}, msg)
}

func (g *gitStorageBackend) BulkPut(ctx context.Context, reqs []PutRequest, msg string) error {
	blobs := make([]git.Hash, len(reqs))
	for i, req := range reqs {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(req.Value); err != nil {
			return fmt.Errorf("encode [%d] JSON: %w", i, err)
		}

		blobHash, err := g.repo.WriteObject(ctx, git.BlobType, &buf)
		if err != nil {
			return fmt.Errorf("write object: %w", err)
		}

		blobs[i] = blobHash
	}

	var updateErr error
	for i := 0; i < 5; i++ {
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

		newTree, err := g.repo.UpdateTree(ctx, git.UpdateTreeRequest{
			Tree: prevTree,
			Writes: func(yield func(git.BlobInfo) bool) {
				for i, req := range reqs {
					if !yield(git.BlobInfo{
						Mode: git.RegularMode,
						Path: req.Key,
						Hash: blobs[i],
					}) {
						break
					}
				}
			},
		})
		if err != nil {
			return fmt.Errorf("update tree: %w", err)
		}

		commitReq := git.CommitTreeRequest{
			Tree:    newTree,
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
			updateErr = err
			g.log.Warn("could not update ref: retrying", "err", err)
			continue
		}

		return nil
	}

	return fmt.Errorf("set ref: %w", updateErr)
}

func (g *gitStorageBackend) Del(ctx context.Context, key string, msg string) error {
	prevCommit, err := g.repo.PeelToCommit(ctx, g.ref)
	if err != nil {
		if errors.Is(err, git.ErrNotExist) {
			return nil // nothing to delete
		}
		return fmt.Errorf("get commit: %w", err)
	}

	prevTree, err := g.repo.PeelToTree(ctx, prevCommit.String())
	if err != nil {
		return fmt.Errorf("get tree: %w", err)
	}

	newTree, err := g.repo.UpdateTree(ctx, git.UpdateTreeRequest{
		Tree: prevTree,
		Deletes: func(yield func(string) bool) {
			yield(key)
		},
	})
	if err != nil {
		return fmt.Errorf("update tree: %w", err)
	}

	newCommit, err := g.repo.CommitTree(ctx, git.CommitTreeRequest{
		Tree:    newTree,
		Parents: []git.Hash{prevCommit},
		Message: msg,
		Author:  &g.sig,
	})
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	if err := g.repo.SetRef(ctx, git.SetRefRequest{
		Ref:     g.ref,
		Hash:    newCommit,
		OldHash: prevCommit,
	}); err != nil {
		return fmt.Errorf("set ref: %w", err)
	}

	return nil
}

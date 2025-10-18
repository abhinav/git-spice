package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"sync"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
)

// GitRepository is the subset of the git.Repository API used by the state package.
type GitRepository interface {
	PeelToCommit(ctx context.Context, ref string) (git.Hash, error)
	PeelToTree(ctx context.Context, ref string) (git.Hash, error)
	HashAt(ctx context.Context, commitish, path string) (git.Hash, error)

	ReadObject(ctx context.Context, typ git.Type, hash git.Hash, dst io.Writer) error
	WriteObject(ctx context.Context, typ git.Type, src io.Reader) (git.Hash, error)

	ListTree(ctx context.Context, tree git.Hash, opts git.ListTreeOptions) iter.Seq2[git.TreeEntry, error]
	CommitTree(ctx context.Context, req git.CommitTreeRequest) (git.Hash, error)
	UpdateTree(ctx context.Context, req git.UpdateTreeRequest) (git.Hash, error)
	MakeTree(ctx context.Context, ents iter.Seq2[git.TreeEntry, error]) (git.Hash, int, error)

	SetRef(ctx context.Context, req git.SetRefRequest) error
}

var _ GitRepository = (*git.Repository)(nil)

// GitBackend implements a storage backend using a Git repository
// reference as the storage medium.
type GitBackend struct {
	repo GitRepository
	ref  string
	sig  git.Signature
	log  *silog.Logger
	mu   sync.RWMutex
}

var _ Backend = (*GitBackend)(nil)

// GitConfig is used to configure a GitBackend.
type GitConfig struct {
	Repo                    GitRepository // required
	Ref                     string        // required
	AuthorName, AuthorEmail string        // required

	Log *silog.Logger
}

// NewGitBackend creates a new GitBackend that stores data
// in the given Git repository.
func NewGitBackend(cfg GitConfig) *GitBackend {
	if cfg.Log == nil {
		cfg.Log = silog.Nop()
	}

	return &GitBackend{
		repo: cfg.Repo,
		ref:  cfg.Ref,
		sig: git.Signature{
			Name:  cfg.AuthorName,
			Email: cfg.AuthorEmail,
		},
		log: cfg.Log,
	}
}

// Keys lists the keys in the store in the given directory.
func (g *GitBackend) Keys(ctx context.Context, dir string) ([]string, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var (
		treeHash git.Hash
		err      error
	)
	if dir == "" {
		treeHash, err = g.repo.PeelToTree(ctx, g.ref)
	} else {
		treeHash, err = g.repo.HashAt(ctx, g.ref, dir)
	}
	if err != nil {
		if errors.Is(err, git.ErrNotExist) {
			return nil, nil // no keys
		}
		return nil, fmt.Errorf("get tree hash: %w", err)
	}

	var keys []string
	for ent, err := range g.repo.ListTree(ctx, treeHash, git.ListTreeOptions{Recurse: true}) {
		if err != nil {
			return nil, fmt.Errorf("list tree: %w", err)
		}

		if ent.Type != git.BlobType {
			continue
		}

		keys = append(keys, ent.Name)
	}

	return keys, nil
}

// Get retrieves a value from the store and decodes it into v.
func (g *GitBackend) Get(ctx context.Context, key string, v any) error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	blobHash, err := g.repo.HashAt(ctx, g.ref, key)
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

// Clear removes all keys from the store.
func (g *GitBackend) Clear(ctx context.Context, msg string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	prevCommit, err := g.repo.PeelToCommit(ctx, g.ref)
	if err != nil {
		prevCommit = "" // not initialized
	}

	tree, _, err := g.repo.MakeTree(ctx, func(func(git.TreeEntry, error) bool) {})
	if err != nil {
		return fmt.Errorf("make tree: %w", err)
	}

	commitReq := git.CommitTreeRequest{
		Tree:      tree,
		Message:   msg,
		Author:    &g.sig,
		Committer: &g.sig,
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

// Update applies a batch of changes to the store.
func (g *GitBackend) Update(ctx context.Context, req UpdateRequest) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	setBlobs := make([]git.Hash, len(req.Sets))
	for i, set := range req.Sets {
		must.NotBeBlankf(set.Key, "key must not be blank")

		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(set.Value); err != nil {
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
			Deletes: req.Deletes,
		})
		if err != nil {
			return fmt.Errorf("update tree: %w", err)
		}

		// The tree didn't change, so we don't need to commit.
		if prevTree == newTree {
			return nil
		}

		commitReq := git.CommitTreeRequest{
			Tree:      newTree,
			Message:   req.Message,
			Author:    &g.sig,
			Committer: &g.sig,
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
			g.log.Warn("could not update ref: retrying", "error", err)
			continue
		}

		return nil
	}

	return fmt.Errorf("set ref: %w", updateErr)
}

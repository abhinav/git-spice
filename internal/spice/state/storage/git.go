package storage

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"slices"
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
	return g.keys(ctx, g.ref, dir)
}

func (g *GitBackend) keys(
	ctx context.Context,
	treeish, dir string,
) ([]string, error) {
	var (
		treeHash git.Hash
		err      error
	)
	if dir == "" {
		treeHash, err = g.repo.PeelToTree(ctx, treeish)
	} else {
		treeHash, err = g.repo.HashAt(ctx, treeish, dir)
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

	return g.read(ctx, g.ref, key, v)
}

// Snapshot returns a read-only view of the current Git-backed store revision.
func (g *GitBackend) Snapshot(ctx context.Context) (Snapshot, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	commit, err := g.repo.PeelToCommit(ctx, g.ref)
	if err != nil {
		if errors.Is(err, git.ErrNotExist) {
			return &gitSnapshot{backend: g}, nil
		}
		return nil, fmt.Errorf("get store commit: %w", err)
	}
	tree, err := g.repo.PeelToTree(ctx, commit.String())
	if err != nil {
		return nil, fmt.Errorf("get tree for %v: %w", commit, err)
	}
	return &gitSnapshot{
		backend: g,
		commit:  commit,
		tree:    tree,
	}, nil
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

// CompareAndSwap applies req if snapshot still identifies the backing ref.
func (g *GitBackend) CompareAndSwap(
	ctx context.Context,
	snapshot Snapshot,
	req UpdateRequest,
) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	snap, ok := snapshot.(*gitSnapshot)
	if !ok || snap.backend != g {
		return ErrInvalidSnapshot
	}
	current, err := g.repo.PeelToCommit(ctx, g.ref)
	if err != nil {
		if !errors.Is(err, git.ErrNotExist) {
			return fmt.Errorf("get store commit: %w", err)
		}
		current = ""
	}
	if current != snap.commit {
		return ErrConflict
	}
	setBlobs, err := g.writeSets(ctx, req.Sets)
	if err != nil {
		return err
	}
	return g.update(ctx, req, setBlobs, snap.commit, snap.tree)
}

// Update applies req to the current Git-backed store revision.
func (g *GitBackend) Update(ctx context.Context, req UpdateRequest) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	setBlobs, err := g.writeSets(ctx, req.Sets)
	if err != nil {
		return err
	}

	var updateErr error
	for range 5 {
		var prevTree git.Hash
		prevCommit, err := g.repo.PeelToCommit(ctx, g.ref)
		if err != nil {
			if !errors.Is(err, git.ErrNotExist) {
				return fmt.Errorf("get store commit: %w", err)
			}
			prevCommit = ""
			prevTree = ""
		} else {
			prevTree, err = g.repo.PeelToTree(ctx, prevCommit.String())
			if err != nil {
				return fmt.Errorf("get tree for %v: %w", prevCommit, err)
			}
		}

		err = g.update(ctx, req, setBlobs, prevCommit, prevTree)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrConflict) {
			return err
		}
		updateErr = err
		g.log.Warn("could not update ref: retrying", "error", err)
	}

	return fmt.Errorf("set ref: %w", updateErr)
}

func (g *GitBackend) update(
	ctx context.Context,
	req UpdateRequest,
	setBlobs []git.Hash,
	prevCommit, prevTree git.Hash,
) error {
	writesByPath := make(map[string]git.Hash, len(req.Sets)+len(req.Moves))
	deletes := make(map[string]struct{}, len(req.Deletes)+len(req.Moves))
	for _, move := range req.Moves {
		var blob git.Hash
		if prevTree != "" {
			var err error
			blob, err = g.repo.HashAt(ctx, prevTree.String(), move.From)
			if err != nil && !errors.Is(err, git.ErrNotExist) {
				return fmt.Errorf("read moved value %q: %w", move.From, err)
			}
		}
		if blob == "" {
			deletes[move.To] = struct{}{}
		} else {
			writesByPath[move.To] = blob
			delete(deletes, move.To)
		}
		delete(writesByPath, move.From)
		deletes[move.From] = struct{}{}
	}
	for i, set := range req.Sets {
		writesByPath[set.Key] = setBlobs[i]
		delete(deletes, set.Key)
	}
	for _, key := range req.Deletes {
		delete(writesByPath, key)
		deletes[key] = struct{}{}
	}

	writes := make([]git.BlobInfo, 0, len(writesByPath))
	for _, path := range slices.Sorted(maps.Keys(writesByPath)) {
		writes = append(writes, git.BlobInfo{
			Mode: git.RegularMode,
			Path: path,
			Hash: writesByPath[path],
		})
	}
	newTree, err := g.repo.UpdateTree(ctx, git.UpdateTreeRequest{
		Tree:    prevTree,
		Writes:  writes,
		Deletes: slices.Sorted(maps.Keys(deletes)),
	})
	if err != nil {
		return fmt.Errorf("update tree: %w", err)
	}
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
		return ErrConflict
	}
	return nil
}

func (g *GitBackend) read(
	ctx context.Context,
	commitish string,
	key string,
	dst any,
) error {
	blobHash, err := g.repo.HashAt(ctx, commitish, key)
	if err != nil {
		return ErrNotExist
	}

	var buf bytes.Buffer
	if err := g.repo.ReadObject(ctx, git.BlobType, blobHash, &buf); err != nil {
		return fmt.Errorf("read object: %w", err)
	}
	if err := json.UnmarshalRead(&buf, dst); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	return nil
}

func (g *GitBackend) writeSets(
	ctx context.Context,
	sets []SetRequest,
) ([]git.Hash, error) {
	blobs := make([]git.Hash, len(sets))
	for i, set := range sets {
		must.NotBeBlankf(set.Key, "key must not be blank")

		var buf bytes.Buffer
		enc := jsontext.NewEncoder(
			&buf,
			jsontext.WithIndent("  "),
		)
		if err := json.MarshalEncode(enc, set.Value); err != nil {
			return nil, fmt.Errorf("encode JSON: %w", err)
		}

		blob, err := g.repo.WriteObject(ctx, git.BlobType, &buf)
		if err != nil {
			return nil, fmt.Errorf("write object: %w", err)
		}
		blobs[i] = blob
	}
	return blobs, nil
}

type gitSnapshot struct {
	backend *GitBackend
	commit  git.Hash
	tree    git.Hash
}

var _ Snapshot = (*gitSnapshot)(nil)

func (s *gitSnapshot) Get(
	ctx context.Context,
	key string,
	dst any,
) error {
	if s.commit == "" {
		return ErrNotExist
	}
	return s.backend.read(ctx, s.commit.String(), key, dst)
}

func (s *gitSnapshot) Keys(
	ctx context.Context,
	dir string,
) ([]string, error) {
	if s.commit == "" {
		return nil, nil
	}
	return s.backend.keys(ctx, s.commit.String(), dir)
}

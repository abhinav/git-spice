package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"go.abhg.dev/gs/internal/osutil"
)

// Mode is the octal file mode of a Git tree entry.
type Mode int

const (
	ZeroMode    Mode = 0o000000
	RegularMode Mode = 0o100644
	DirMode     Mode = 0o40000
)

func ParseMode(s string) (Mode, error) {
	i, err := strconv.ParseInt(s, 8, 32)
	return Mode(i), err
}

func (m Mode) String() string {
	return fmt.Sprintf("%06o", m)
}

type TreeEntry struct {
	Mode Mode
	Type Type
	Hash Hash
	Name string
}

func (r *Repository) MakeTree(ctx context.Context, ents iter.Seq[TreeEntry]) (_ Hash, err error) {
	var stdout bytes.Buffer
	cmd := r.gitCmd(ctx, "mktree").Stdout(&stdout)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return ZeroHash, fmt.Errorf("pipe: %w", err)
	}

	if err := cmd.Start(r.exec); err != nil {
		return ZeroHash, fmt.Errorf("start: %w", err)
	}
	defer func() {
		if err != nil {
			_ = cmd.Kill(r.exec)
		}
	}()

	for ent := range ents {
		if ent.Type == "" {
			return ZeroHash, fmt.Errorf("type not set for %q", ent.Name)
		}
		if strings.Contains(ent.Name, "/") {
			return ZeroHash, fmt.Errorf("name %q contains a slash", ent.Name)
		}

		// mktree expects input in the form:
		//	<mode> SP <type> SP <hash> TAB <name> NL
		_, err := fmt.Fprintf(stdin, "%s %s %s\t%s\n", ent.Mode, ent.Type, ent.Hash, ent.Name)
		if err != nil {
			return ZeroHash, fmt.Errorf("write: %w", err)
		}
	}

	if err := stdin.Close(); err != nil {
		return ZeroHash, fmt.Errorf("close: %w", err)
	}

	if err := cmd.Wait(r.exec); err != nil {
		return ZeroHash, fmt.Errorf("wait: %w", err)
	}

	return Hash(bytes.TrimSpace(stdout.Bytes())), nil
}

type ListTreeOptions struct {
	Recurse bool
}

func (r *Repository) ListTree(ctx context.Context, tree Hash, opts ListTreeOptions) (iter.Seq2[TreeEntry, error], error) {
	args := []string{
		"ls-tree",
		"--full-tree", // don't limit listing to the current working directory
	}
	if opts.Recurse {
		args = append(args, "-r")
	}
	args = append(args, tree.String())

	cmd := r.gitCmd(ctx, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}

	if err := cmd.Start(r.exec); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	scanner := bufio.NewScanner(stdout)

	return func(yield func(ent TreeEntry, err error) bool) {
		var finished bool // whether we ran to completion
		defer func() {
			if finished {
				return
			}

			// If we stopped early, kill the command and consume
			// its output.
			_ = cmd.Kill(r.exec)
			_, _ = io.Copy(io.Discard, stdout)
		}()

		for scanner.Scan() {
			line := scanner.Bytes()
			// ls-tree output is in the form:
			//	<mode> SP <type> SP <hash> TAB <name> NL
			modeTypeHash, name, ok := bytes.Cut(line, []byte{'\t'})
			if !ok {
				r.log.Printf("ls-tree: skipping invalid line: %q", line)
				continue
			}

			toks := bytes.SplitN(modeTypeHash, []byte{' '}, 3)
			if len(toks) != 3 {
				r.log.Printf("ls-tree: skipping invalid line: %q", line)
				continue
			}

			mode, err := ParseMode(string(toks[0]))
			if err != nil {
				r.log.Printf("ls-tree: skipping invalid mode: %q: %v", toks[0], err)
				continue
			}

			ok = yield(TreeEntry{
				Mode: mode,
				Type: Type(toks[1]),
				Hash: Hash(toks[2]),
				Name: string(name),
			}, nil)
			if !ok {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			if !yield(TreeEntry{}, fmt.Errorf("scan: %w", err)) {
				return
			}
		}

		if err := cmd.Wait(r.exec); err != nil {
			if !yield(TreeEntry{}, fmt.Errorf("wait: %w", err)) {
				return
			}
		}

		finished = true
	}, nil
}

// UpdateTreeRequest is a request to update an existing Git tree.
// Unlike MakeTree, it's able to operate on paths with slashes.
type UpdateTreeRequest struct {
	Tree    Hash
	Writes  iter.Seq[BlobInfo]
	Deletes iter.Seq[string]
}

// UpdateTree updates the given tree with the given writes and deletes,
// returning the new tree hash.
func (r *Repository) UpdateTree(ctx context.Context, req UpdateTreeRequest) (_ Hash, err error) {
	// Use a temporary index file to update the tree.
	indexFile, err := osutil.TempFilePath("", "gs-index-*")
	if err != nil {
		return ZeroHash, fmt.Errorf("create index: %w", err)
	}
	defer func() {
		err = errors.Join(err, os.Remove(indexFile))
	}()

	err = r.gitCmd(ctx, "read-tree", "--index-output", indexFile, req.Tree.String()).
		Run(r.exec)
	if err != nil {
		return ZeroHash, fmt.Errorf("read-tree: %w", err)
	}

	updateCmd := r.gitCmd(ctx, "update-index", "--index-info").
		AppendEnv("GIT_INDEX_FILE=" + indexFile)
	stdin, err := updateCmd.StdinPipe()
	if err != nil {
		return ZeroHash, fmt.Errorf("create pipe: %w", err)
	}
	if err := updateCmd.Start(r.exec); err != nil {
		return ZeroHash, fmt.Errorf("start: %w", err)
	}

	if req.Writes != nil {
		for blob := range req.Writes {
			// update-index accepts input in the form:
			//   <mode> SP <sha1> TAB <path> NL
			if blob.Mode == ZeroMode {
				blob.Mode = RegularMode
			}

			if _, err := fmt.Fprintf(stdin, "%s %s\t%s\n", blob.Mode, blob.Hash, blob.Path); err != nil {
				return ZeroHash, fmt.Errorf("write: %w", err)
			}
		}
	}

	if req.Deletes != nil {
		for path := range req.Deletes {
			// For deletes, we need to use 000000 as the mode,
			// and hash does not matter.
			if _, err := fmt.Fprintf(stdin, "000000 %s\t%s\n", ZeroHash, path); err != nil {
				return ZeroHash, fmt.Errorf("delete: %w", err)
			}
		}
	}

	if err := stdin.Close(); err != nil {
		return ZeroHash, fmt.Errorf("close: %w", err)
	}

	if err := updateCmd.Wait(r.exec); err != nil {
		return ZeroHash, fmt.Errorf("wait: %w", err)
	}

	// Write the updated index to a new tree.
	treeCmd := r.gitCmd(ctx, "write-tree").
		AppendEnv("GIT_INDEX_FILE=" + indexFile)
	treeHash, err := treeCmd.OutputString(r.exec)
	if err != nil {
		return ZeroHash, fmt.Errorf("write-tree: %w", err)
	}

	return Hash(treeHash), nil
}

type TreeMaker interface {
	MakeTree(ctx context.Context, ents iter.Seq[TreeEntry]) (Hash, error)
}

type BlobInfo struct {
	Mode Mode
	Hash Hash
	Path string
}

// MakeTreeRecursive is a variant of MakeTree that supports creating subtrees.
func MakeTreeRecursive(ctx context.Context, tm TreeMaker, blobs iter.Seq[BlobInfo]) (Hash, error) {
	var root treeTreeNode
	for blob := range blobs {
		dir, name := path.Split(blob.Path)
		parent, err := root.getSubtree(dir)
		if err != nil {
			return ZeroHash, fmt.Errorf("subtree %q: %w", dir, err)
		}

		parent.putBlob(name, blob.Mode, blob.Hash)
	}

	return root.make(ctx, tm)
}

type treeNode interface {
	name() string
	typ() Type
}

type treeBlobNode struct {
	Name string
	Mode Mode
	Hash Hash
}

func (b *treeBlobNode) name() string { return b.Name }
func (b *treeBlobNode) typ() Type    { return BlobType }

type treeTreeNode struct {
	Name  string
	Items []treeNode // sorted by name
}

func (t *treeTreeNode) name() string { return t.Name }
func (t *treeTreeNode) typ() Type    { return TreeType }

func (t *treeTreeNode) make(ctx context.Context, tm TreeMaker) (_ Hash, retErr error) {
	return tm.MakeTree(ctx, func(yield func(TreeEntry) bool) {
		for _, item := range t.Items {
			ent := TreeEntry{
				Name: item.name(),
				Type: item.typ(),
			}

			switch item := item.(type) {
			case *treeBlobNode:
				ent.Mode = item.Mode
				ent.Hash = item.Hash

			case *treeTreeNode:
				hash, err := item.make(ctx, tm)
				if err != nil {
					retErr = errors.Join(retErr, fmt.Errorf("subtree %q: %w", item.Name, err))
					return
				}

				ent.Mode = DirMode
				ent.Hash = hash
			}

			if !yield(ent) {
				return
			}
		}
	})
}

// getSubtree gets the subtree at the given path.
func (t *treeTreeNode) getSubtree(p string) (*treeTreeNode, error) {
	if p == "" {
		return t, nil
	}

	name, rest, _ := strings.Cut(p, "/")
	idx, ok := slices.BinarySearchFunc(t.Items, name, func(n treeNode, name string) int {
		return strings.Compare(n.name(), name)
	})
	var sub *treeTreeNode
	if ok {
		sub, ok = t.Items[idx].(*treeTreeNode)
		if !ok {
			return nil, fmt.Errorf("expected tree, got %T", t.Items[idx])
		}
	} else {
		// Not found. Create a new subtree.
		sub = &treeTreeNode{Name: name}
		t.Items = slices.Insert(t.Items, idx, treeNode(sub))
	}

	return sub.getSubtree(rest)
}

func (t *treeTreeNode) putBlob(name string, mode Mode, hash Hash) {
	node := &treeBlobNode{Name: name, Mode: mode, Hash: hash}

	idx, ok := slices.BinarySearchFunc(t.Items, name, func(n treeNode, name string) int {
		return strings.Compare(n.name(), name)
	})
	if ok {
		t.Items[idx] = node
	} else {
		t.Items = slices.Insert(t.Items, idx, treeNode(node))
	}
}

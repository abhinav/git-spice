package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/scanutil"
)

// Mode is the octal file mode of a Git tree entry.
type Mode int

// List of modes that git-spice cares about.
// Git recognizes a few more, but we don't use them.
const (
	ZeroMode    Mode = 0o000000
	RegularMode Mode = 0o100644
	DirMode     Mode = 0o40000
)

// ParseMode parses a Git tree entry mode from a string.
// These strings are octal numbers, e.g.
//
//	100644
//	040000
//
// Git only recognizes a handful of values for this,
// but we don't enforce that here.
func ParseMode(s string) (Mode, error) {
	i, err := strconv.ParseInt(s, 8, 32)
	return Mode(i), err
}

func (m Mode) String() string {
	return fmt.Sprintf("%06o", m)
}

// TreeEntry is a single entry in a Git tree.
type TreeEntry struct {
	// Mode is the file mode of the entry.
	//
	// For regular files, this is RegularMode.
	// For directories, this is DirMode.
	Mode Mode

	// Type is the type of the entry.
	//
	// This is either BlobType or TreeType.
	Type Type

	// Hash is the hash of the entry.
	Hash Hash

	// Name is the name of the entry.
	Name string
}

// MakeTree creates a new Git tree from the given entries.
//
// The tree will contain *only* the given entries and nothing else.
// Entries must not contain slashes in their names;
// this operation does not create subtrees.
//
// Returns the hash of the new tree and the number of entries written.
func (r *Repository) MakeTree(ctx context.Context, ents iter.Seq2[TreeEntry, error]) (_ Hash, numEnts int, err error) {
	var stdout bytes.Buffer
	cmd := r.gitCmd(ctx, "mktree", "-z").WithStdout(&stdout)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return ZeroHash, numEnts, fmt.Errorf("pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return ZeroHash, numEnts, fmt.Errorf("start: %w", err)
	}
	defer func() {
		if err != nil {
			_ = cmd.Kill()
		}
	}()

	for ent, err := range ents {
		if err != nil {
			return ZeroHash, numEnts, fmt.Errorf("read entry: %w", err)
		}

		if ent.Type == "" {
			return ZeroHash, numEnts, fmt.Errorf("type not set for %q", ent.Name)
		}

		if ent.Mode == ZeroMode {
			switch ent.Type {
			case BlobType:
				ent.Mode = RegularMode
			case TreeType:
				ent.Mode = DirMode
			default:
				return ZeroHash, numEnts, fmt.Errorf("mode not set for %q", ent.Name)
			}
		}

		if strings.Contains(ent.Name, "/") {
			return ZeroHash, numEnts, fmt.Errorf("name %q contains a slash", ent.Name)
		}

		// mktree -z expects input in the form:
		//	<mode> SP <type> SP <hash> TAB <name> NUL
		_, err := fmt.Fprintf(stdin, "%s %s %s\t%s\x00", ent.Mode, ent.Type, ent.Hash, ent.Name)
		if err != nil {
			return ZeroHash, numEnts, fmt.Errorf("write: %w", err)
		}

		numEnts++
	}

	if err := stdin.Close(); err != nil {
		return ZeroHash, numEnts, fmt.Errorf("close: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return ZeroHash, numEnts, fmt.Errorf("wait: %w", err)
	}

	return Hash(bytes.TrimSpace(stdout.Bytes())), numEnts, nil
}

// ListTreeOptions specifies options for the ListTree operation.
type ListTreeOptions struct {
	// Recurse specifies whether subtrees should be expanded.
	Recurse bool
}

// ListTree lists the entries in the given tree.
//
// By default, the returned entries will only include the immediate children of the tree.
// Subdirectories will be listed as tree objects, and have to be expanded manually.
//
// If opts.Recurse is true, this operation will expand all subtrees.
// The returned entries will only include blobs,
// and their path will be the full path relative to the root of the tree.
func (r *Repository) ListTree(
	ctx context.Context,
	tree Hash,
	opts ListTreeOptions,
) iter.Seq2[TreeEntry, error] {
	args := []string{
		"ls-tree",
		"-z",          // NUL-terminate entries for proper handling of special characters
		"--full-tree", // don't limit listing to the current working directory
	}
	if opts.Recurse {
		args = append(args, "-r")
	}
	args = append(args, tree.String())

	return func(yield func(TreeEntry, error) bool) {
		cmd := r.gitCmd(ctx, args...)
		for line, err := range cmd.Scan(scanutil.SplitNull) {
			if err != nil {
				yield(TreeEntry{}, fmt.Errorf("git ls-tree: %w", err))
				return
			}

			// ls-tree -z output is in the form:
			//	<mode> SP <type> SP <hash> TAB <name> NUL
			modeTypeHash, name, ok := bytes.Cut(line, []byte{'\t'})
			if !ok {
				r.log.Warnf("ls-tree: skipping invalid line: %q", line)
				continue
			}

			toks := bytes.SplitN(modeTypeHash, []byte{' '}, 3)
			if len(toks) != 3 {
				r.log.Warnf("ls-tree: skipping invalid line: %q", line)
				continue
			}

			mode, err := ParseMode(string(toks[0]))
			if err != nil {
				r.log.Warnf("ls-tree: skipping invalid mode: %q: %v", toks[0], err)
				continue
			}

			if !yield(TreeEntry{
				Mode: mode,
				Type: Type(toks[1]),
				Hash: Hash(toks[2]),
				Name: string(name),
			}, nil) {
				return
			}
		}
	}
}

// UpdateTreeRequest is a request to update an existing Git tree.
//
// Unlike MakeTree, it's able to operate on paths with slashes.
type UpdateTreeRequest struct {
	// Tree is the starting tree hash.
	//
	// This may be empty or [ZeroHash] to start with an empty tree.
	Tree Hash

	// Writes is a sequence of blobs to write to the tree.
	Writes []BlobInfo

	// Deletes is a set of paths to delete from the tree.
	Deletes []string
}

// BlobInfo is a single blob in a tree.
type BlobInfo struct {
	// Mode is the file mode of the blob.
	//
	// Defaults to [RegularMode] if unset.
	Mode Mode

	// Hash is the hash of the blob.
	Hash Hash

	// Path is the path to the blob relative to the tree root.
	// If it contains slashes, intermediate directories will be created.
	Path string
}

// UpdateTree updates an existing Git tree with differential changes to blobs
// and returns the hash of the new tree.
func (r *Repository) UpdateTree(ctx context.Context, req UpdateTreeRequest) (_ Hash, err error) {
	if len(req.Writes) == 0 && len(req.Deletes) == 0 {
		return req.Tree, nil
	}
	// We have a list of path updates. We need to take the following steps:
	// 1. Group updates by directory.
	// 2. Enumerate all intermediate directories for each update.
	// 3. Starting with the deepest directory:
	//     a. Read the current tree for that directory
	//     b. Apply updates to the tree
	//     c. Write the updated tree
	//     d. Add the new hash of the tree to updates
	//        for the parent directory
	// 4. Return the hash of the root directory.

	// Tracks all directories that are affected by updates
	// up to the root directory.
	affectedDirs := make(map[string]struct{})
	affectDir := func(p string) {
		must.NotBeBlankf(p, "path must not be blank")

		for p != "." {
			affectedDirs[p] = struct{}{}
			p = path.Dir(p)
		}
	}

	updates := make(map[string]*directoryUpdate)
	ensureUpdate := func(dir string) *directoryUpdate {
		dir = strings.TrimSuffix(dir, "/")
		if dir == "" {
			dir = "."
		}

		affectDir(dir)
		u, ok := updates[dir]
		if !ok {
			u = new(directoryUpdate)
			updates[dir] = u
		}
		return u
	}

	for _, blob := range req.Writes {
		dir, name := pathSplit(blob.Path)
		if blob.Mode == ZeroMode {
			blob.Mode = RegularMode
		}
		ensureUpdate(dir).Put(TreeEntry{
			Mode: blob.Mode,
			Type: BlobType,
			Hash: blob.Hash,
			Name: name,
		})
	}

	for _, p := range req.Deletes {
		dir, name := pathSplit(p)
		ensureUpdate(dir).Delete(name)
	}

	// Sort the directories by depth, so we can process them in order.
	dirs := slices.SortedFunc(maps.Keys(affectedDirs), func(a, b string) int {
		diff := strings.Count(b, "/") - strings.Count(a, "/")
		if diff == 0 {
			diff = strings.Compare(a, b)
		}
		return diff
	})

	for _, dir := range dirs {
		update := updates[dir]
		delete(updates, dir)
		if update.Empty() {
			// This directory has no updates.
			continue
		}

		oldHash, err := r.HashAt(ctx, req.Tree.String(), dir)
		if err != nil {
			if !errors.Is(err, ErrNotExist) {
				return ZeroHash, fmt.Errorf("hash %v:%q: %w", req.Tree, dir, err)
			}
			oldHash = ZeroHash
		}

		var entries iter.Seq2[TreeEntry, error]
		if oldHash != ZeroHash {
			entries = r.ListTree(ctx, oldHash, ListTreeOptions{})
		} else {
			entries = func(func(TreeEntry, error) bool) {}
		}
		entries = update.Apply(entries)

		newHash, numEnts, err := r.MakeTree(ctx, entries)
		if err != nil {
			return ZeroHash, fmt.Errorf("make tree: %w", err)
		}
		if numEnts == 0 {
			// If the directory is empty, delete the directory
			// from the parent.
			parent, base := pathSplit(dir)
			ensureUpdate(parent).Delete(base)
			continue
		}

		// Update the parent directory with the new hash.
		parent, base := pathSplit(dir)
		ensureUpdate(parent).Put(TreeEntry{
			Mode: DirMode,
			Type: TreeType,
			Hash: newHash,
			Name: base,
		})
	}

	// Process root directory separately.
	var entries iter.Seq2[TreeEntry, error]
	if req.Tree != ZeroHash && req.Tree != "" {
		entries = r.ListTree(ctx, req.Tree, ListTreeOptions{})
	} else {
		entries = func(func(TreeEntry, error) bool) {}
	}

	rootHash, _, err := r.MakeTree(ctx, updates["."].Apply(entries))
	if err != nil {
		return ZeroHash, fmt.Errorf("make root tree: %w", err)
	}
	delete(updates, ".")

	// If there are any updates that we didn't look at,
	// we have a bug in our code and we should fail loudly.
	must.BeEmptyMapf(updates, "unapplied updates")
	return rootHash, nil
}

type directoryUpdate struct {
	Writes  []TreeEntry // sorted by name
	Deletes []string    // sorted
}

func (du *directoryUpdate) Apply(entries iter.Seq2[TreeEntry, error]) iter.Seq2[TreeEntry, error] {
	if du == nil {
		return entries
	}

	return func(yield func(TreeEntry, error) bool) {
		for ent, err := range entries {
			if err != nil {
				yield(TreeEntry{}, err)
				return
			}

			if idx, ok := slices.BinarySearch(du.Deletes, ent.Name); ok {
				du.Deletes = slices.Delete(du.Deletes, idx, idx+1)
				continue
			}

			if idx, ok := slices.BinarySearchFunc(du.Writes, ent.Name, entryByName); ok {
				ent = du.Writes[idx]
				du.Writes = slices.Delete(du.Writes, idx, idx+1)
			}

			if !yield(ent, nil) {
				return
			}
		}

		// If there are any more writes remaining,
		// they are new entries.
		for _, ent := range du.Writes {
			if !yield(ent, nil) {
				return
			}
		}
	}
}

func (du *directoryUpdate) Empty() bool {
	return du == nil || len(du.Writes) == 0 && len(du.Deletes) == 0
}

func (du *directoryUpdate) Put(ent TreeEntry) {
	must.NotBeBlankf(ent.Name, "name must not be blank")
	must.NotBeEqualf(ent.Name, ".", "name must not be .")

	idx, ok := slices.BinarySearchFunc(du.Writes, ent.Name, entryByName)
	if ok {
		du.Writes[idx] = ent
	} else {
		du.Writes = slices.Insert(du.Writes, idx, ent)
	}
}

func (du *directoryUpdate) Delete(name string) {
	must.NotBeBlankf(name, "name must not be blank")

	idx, ok := slices.BinarySearch(du.Deletes, name)
	if !ok {
		du.Deletes = slices.Insert(du.Deletes, idx, name)
	}
}

func entryByName(ent TreeEntry, name string) int {
	return strings.Compare(ent.Name, name)
}

func pathSplit(p string) (dir, name string) {
	return path.Dir(p), path.Base(p)
}

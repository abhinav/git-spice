package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"

	"go.abhg.dev/gs/internal/maputil"
	"go.abhg.dev/gs/internal/must"
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
// The tree will contain *only* the given entries and nothing else.
// Entries must not contain slashes in their names;
// this operation does not create subtrees.
func (r *Repository) MakeTree(ctx context.Context, ents []TreeEntry) (_ Hash, err error) {
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

	for _, ent := range ents {
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
) (_ []TreeEntry, err error) {
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
	defer func() {
		if err != nil {
			_ = cmd.Kill(r.exec)
			_, _ = io.Copy(io.Discard, stdout)
		}
	}()

	scanner := bufio.NewScanner(stdout)
	var ents []TreeEntry
	for scanner.Scan() {
		line := scanner.Bytes()
		// ls-tree output is in the form:
		//	<mode> SP <type> SP <hash> TAB <name> NL
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

		ents = append(ents, TreeEntry{
			Mode: mode,
			Type: Type(toks[1]),
			Hash: Hash(toks[2]),
			Name: string(name),
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	if err := cmd.Wait(r.exec); err != nil {
		return nil, fmt.Errorf("git ls-tree: %w", err)
	}

	return ents, nil
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
	dirs := maputil.Keys(affectedDirs)
	slices.SortFunc(dirs, func(a, b string) int {
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

		var entries []TreeEntry
		if oldHash != ZeroHash {
			entries, err = r.ListTree(ctx, oldHash, ListTreeOptions{})
			if err != nil {
				return ZeroHash, fmt.Errorf("list %v (%v): %w", dir, oldHash, err)
			}
		}

		newHash, err := r.MakeTree(ctx, update.Apply(entries))
		if err != nil {
			return ZeroHash, fmt.Errorf("make tree: %w", err)
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
	var entries []TreeEntry
	if req.Tree != ZeroHash && req.Tree != "" {
		entries, err = r.ListTree(ctx, req.Tree, ListTreeOptions{})
		if err != nil {
			return ZeroHash, fmt.Errorf("list root (%v): %w", req.Tree, err)
		}
	}

	rootHash, err := r.MakeTree(ctx, updates["."].Apply(entries))
	if err != nil {
		return ZeroHash, fmt.Errorf("make root tree: %w", err)
	}
	delete(updates, ".")

	// If there are any updates that we didn't look at,
	// we have a bug in our code and we should fail loudly.
	if len(updates) > 0 {
		var msg strings.Builder
		msg.WriteString("unapplied updates:")
		for dir := range updates {
			msg.WriteString(" ")
			msg.WriteString(dir)
		}
		must.Failf(msg.String())
	}

	return rootHash, nil
}

type directoryUpdate struct {
	Writes  []TreeEntry // sorted by name
	Deletes []string    // sorted
}

func (du *directoryUpdate) Apply(entries []TreeEntry) []TreeEntry {
	if du == nil {
		return entries
	}

	newEntries := entries[:0]
	for _, ent := range entries {
		if idx, ok := slices.BinarySearch(du.Deletes, ent.Name); ok {
			du.Deletes = slices.Delete(du.Deletes, idx, idx+1)
			continue
		}

		if idx, ok := slices.BinarySearchFunc(du.Writes, ent.Name, entryByName); ok {
			ent = du.Writes[idx]
			du.Writes = slices.Delete(du.Writes, idx, idx+1)
		}

		newEntries = append(newEntries, ent)
	}

	// If there are any more writes remaining,
	// they are new entries.
	newEntries = append(newEntries, du.Writes...)
	return newEntries
}

func (du *directoryUpdate) Empty() bool {
	return du == nil || len(du.Writes) == 0 && len(du.Deletes) == 0
}

func (du *directoryUpdate) Put(ent TreeEntry) {
	must.NotBeBlankf(ent.Name, "name must not be blank")

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

package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"go.abhg.dev/git-spice/internal/osutil"
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

// UpdateTree updates the given tree with the given writes and deletes,
// returning the new tree hash.
func (r *Repository) UpdateTree(ctx context.Context, req UpdateTreeRequest) (_ Hash, err error) {
	// Use a temporary index file to update the tree.
	indexFile, err := osutil.TempFilePath("", "spice-index-*")
	if err != nil {
		return ZeroHash, fmt.Errorf("create index: %w", err)
	}
	defer func() {
		err = errors.Join(err, os.Remove(indexFile))
	}()

	readTreeArgs := []string{"read-tree", "--index-output", indexFile}
	if req.Tree == ZeroHash || req.Tree == "" {
		readTreeArgs = append(readTreeArgs, "--empty")
	} else {
		readTreeArgs = append(readTreeArgs, req.Tree.String())
	}

	if err := r.gitCmd(ctx, readTreeArgs...).Run(r.exec); err != nil {
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
		for _, blob := range req.Writes {
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
		for _, path := range req.Deletes {
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
	return r.writeIndexToTree(ctx, indexFile)
}

// writeIndexToTree writes the given Git index file into a new tree object.
func (r *Repository) writeIndexToTree(ctx context.Context, index string) (_ Hash, err error) {
	cmd := r.gitCmd(ctx, "write-tree")
	if index != "" {
		cmd = cmd.AppendEnv("GIT_INDEX_FILE=" + index)
	}

	treeHash, err := cmd.OutputString(r.exec)
	if err != nil {
		return ZeroHash, fmt.Errorf("write-tree: %w", err)
	}

	return Hash(treeHash), nil
}

package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"

	"go.abhg.dev/gs/internal/scanutil"
	"go.abhg.dev/gs/internal/silog"
)

// FileStatusCode specifies the status of a file in a diff.
type FileStatusCode string

// List of file status codes from
// https://git-scm.com/docs/git-diff-index#Documentation/git-diff-index.txt---diff-filterACDMRTUXB82308203.
const (
	FileUnchanged   FileStatusCode = ""
	FileAdded       FileStatusCode = "A"
	FileCopied      FileStatusCode = "C"
	FileDeleted     FileStatusCode = "D"
	FileModified    FileStatusCode = "M"
	FileRenamed     FileStatusCode = "R"
	FileTypeChanged FileStatusCode = "T"
	FileUnmerged    FileStatusCode = "U"
)

// FileStatus is a single file in a diff.
type FileStatus struct {
	// Status of the file.
	Status string

	// Path to the file relative to the tree root.
	Path string
}

// DiffWork compares the working tree with the index
// and returns an iterator over files that are different.
func (w *Worktree) DiffWork(ctx context.Context) iter.Seq2[FileStatus, error] {
	return func(yield func(FileStatus, error) bool) {
		cmd := w.gitCmd(ctx, "diff-files", "--name-status", "-z")
		var status string
		var expectingPath bool
		for line, err := range cmd.Scan(scanutil.SplitNull) {
			if err != nil {
				yield(FileStatus{}, fmt.Errorf("git diff-files: %w", err))
				return
			}
			if len(line) == 0 {
				continue
			}

			if !expectingPath {
				// First part is the status
				status = string(line)
				expectingPath = true
			} else {
				// Second part is the path
				if !yield(FileStatus{
					Status: status,
					Path:   string(line),
				}, nil) {
					return
				}
				expectingPath = false
			}
		}
	}
}

// DiffIndex compares the index with the given tree
// and returns the list of files that are different.
// The treeish argument can be any valid tree-ish reference.
func (w *Worktree) DiffIndex(ctx context.Context, treeish string) ([]FileStatus, error) {
	cmd := w.gitCmd(ctx, "diff-index", "--cached", "--name-status", treeish)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	files, err := parseDiffFileStatuses(out, w.log)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("diff-index: %w", err)
	}

	return files, nil
}

// DiffTree compares two trees and returns an iterator over files that are different.
// The treeish1 and treeish2 arguments can be any valid tree-ish references.
func (r *Repository) DiffTree(ctx context.Context, treeish1, treeish2 string) iter.Seq2[FileStatus, error] {
	return func(yield func(FileStatus, error) bool) {
		cmd := r.gitCmd(ctx, "diff-tree", "-r", "--name-status", "-z", treeish1, treeish2)
		var status string
		var expectingPath bool
		for line, err := range cmd.Scan(scanutil.SplitNull) {
			if err != nil {
				yield(FileStatus{}, fmt.Errorf("git diff-tree: %w", err))
				return
			}
			if len(line) == 0 {
				continue
			}

			if !expectingPath {
				// First part is the status
				status = string(line)
				expectingPath = true
			} else {
				// Second part is the path
				if !yield(FileStatus{
					Status: status,
					Path:   string(line),
				}, nil) {
					return
				}
				expectingPath = false
			}
		}
	}
}

func parseDiffFileStatuses(r io.Reader, log *silog.Logger) ([]FileStatus, error) {
	var files []FileStatus
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		bs := scanner.Bytes()
		if len(bs) == 0 {
			continue
		}

		status, name, ok := bytes.Cut(bs, []byte{'\t'})
		if !ok {
			log.Warnf("invalid diff: %s", bs)
			continue
		}
		files = append(files, FileStatus{
			Status: string(status),
			Path:   string(name),
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	return files, nil
}

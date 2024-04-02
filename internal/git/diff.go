package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
)

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

type FileStatus struct {
	Status string
	Path   string
}

// DiffIndex compares the index with the given tree
// and returns the list of files that are different.
func (r *Repository) DiffIndex(ctx context.Context, treeish string) ([]FileStatus, error) {
	cmd := r.gitCmd(ctx, "diff-index", "--cached", "--name-status", treeish)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}

	if err := cmd.Start(r.exec); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	var files []FileStatus
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		bs := scanner.Bytes()
		if len(bs) == 0 {
			continue
		}

		status, name, ok := bytes.Cut(bs, []byte{'\t'})
		if !ok {
			log.Printf("invalid diff-index output: %s", bs)
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

	if err := cmd.Wait(r.exec); err != nil {
		return nil, fmt.Errorf("diff-index: %w", err)
	}

	return files, nil
}

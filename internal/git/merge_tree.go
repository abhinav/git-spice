package git

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"go.abhg.dev/gs/internal/scanutil"
)

// MergeTreeRequest specifies the parameters for a merge-tree operation.
type MergeTreeRequest struct {
	// Branch1 is the first branch or commit to merge.
	//
	// This must be a commit-ish value if MergeBase is not provided.
	// Otherwise, it can be any tree-ish value.
	Branch1 string // required

	// Branch2 is the second branch or commit to merge.
	//
	// This must be a commit-ish value if MergeBase is not provided.
	// Otherwise, it can be any tree-ish value.
	Branch2 string // required

	// MergeBase optionally specifies an explicit merge base for the merge.
	// If provided, Branch1 and Branch2 can be any tree-ish values.
	// The difference between this and Branch1 will be applied to Branch2.
	//
	// Use of this parameter requires Git 2.45 or later.
	MergeBase string
	// NB: The parameter was added in 2.40,
	// but support for tree-ish values was added in 2.45.

	// Test-only option to control conflict marker style
	// to get deterministic output even in tests that run in CI.
	conflictStyle string
}

// MergeTreeConflictError is returned from the MergeTree operation
// when a conflict is encountered.
type MergeTreeConflictError struct {
	// Files is the list of files that are in conflict.
	//
	// This is the authoritative list of conflicting files.
	Files []string

	// Details is a list of detailed messages about the conflicts,
	// as well as conflicts that were resolved automatically
	// (e.g. "Auto-merging <file>").
	//
	// Do not assume len(Details) == len(Files).
	// Do not assume len(Details) > 0 means there are blocking conflicts.
	Details []MergeTreeConflictDetails
}

func (e *MergeTreeConflictError) Error() string {
	var msg strings.Builder
	msg.WriteString("conflicting files:")
	for i, f := range e.Files {
		if i > 0 {
			msg.WriteString(",")
		}
		msg.WriteString(" ")
		msg.WriteString(f)
	}
	return msg.String()
}

// MergeTree performs a merge without touching the index or working tree,
// returning the hash of the resulting tree.
//
// For conflicts, this method returns a [MergeTreeConflictError]
// that reports information about the conflicting files.
// If the conflicts were resolved automatically (e.g. "Auto-merging <file>"),
// and there are no other blocking conflicts, this will NOT return an error.
func (r *Repository) MergeTree(ctx context.Context, req MergeTreeRequest) (_ Hash, retErr error) {
	// TODO: support multiple requests now that we're using stdin
	args := []string{
		"merge-tree",
		"--write-tree", // other mode is deprecated
		"--stdin",      // pass input on stdin
		"--name-only",  // only mention conflicting file names instead of stages and objects
		"-z",
	}

	var stdin strings.Builder
	// Input is in the form:
	//   [<base-commit> -- ]<branch1> <branch2> NL
	if req.MergeBase != "" {
		_, _ = fmt.Fprintf(&stdin, "%v -- ", req.MergeBase)
	}
	_, _ = fmt.Fprintf(&stdin, "%v %v\n", req.Branch1, req.Branch2)

	cmd := r.gitCmd(ctx, args...).StdinString(stdin.String())
	if req.conflictStyle != "" {
		cmd = cmd.WithConfig(extraConfig{MergeConflictStyle: req.conflictStyle})
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(r.exec); err != nil {
		return "", fmt.Errorf("start git-merge-tree: %w", err)
	}

	outputs, err := parseMergeTreeOutput(stdout)
	if err != nil {
		return "", fmt.Errorf("bad git-merge-tree output: %w", err)
	}
	if len(outputs) != 1 {
		return "", fmt.Errorf("expected one result from git-merge-tree, got %d", len(outputs))
	}

	waitErr := cmd.Wait(r.exec) // will use below
	if waitErr != nil {
		waitErr = fmt.Errorf("git merge-tree: %w", err)
	}

	o := outputs[0]
	if len(o.ConflictFiles) == 0 {
		return o.TreeHash, waitErr
	}
	return o.TreeHash, errors.Join(&MergeTreeConflictError{
		Files:   o.ConflictFiles,
		Details: o.ConflictMessages,
	}, waitErr)
}

// mergeTreeOutput holds the output of a git-merge-tree operation
// run with the --write-tree option (this is the non-deprecated variant).
//
// If a conflict was resolved with an auto-merge in Git,
// the output will report as conflicted even though no user action is required.
// So DO NOT assume that there's a blocking conflict without checking for
// Auto-merge messages. Per git-merge-tree documentation:
//
//	Do NOT assume all filenames listed in the Informational messages section had conflicts.
//	Messages can be included for files that have no conflicts, such as "Auto-merging <file>".
type mergeTreeOutput struct {
	// TreeHash is the hash of the resulting tree.
	// There is no other output if there are no conflicts.
	//
	TreeHash Hash

	ConflictFiles    []string
	ConflictMessages []MergeTreeConflictDetails
}

// MergeTreeConflictDetails represents an informational message about a conflict.
type MergeTreeConflictDetails struct {
	// Paths is a list of files affected by this message/kind of conflict.
	Paths []string

	// Type is the type of conflict.
	// This is a stable string like
	// "CONFLICT (rename/delete)", "CONFLICT (binary)", etc.
	// This may be consumed programmatically.
	Type string // TODO: don't surface Auto-merging to users

	// Message is a detailed user-readable message explaining the conflict.
	// This string is not stable and may change between Git versions.
	Message string
}

// parseMergeTreeOutput parses the output of a git merge-tree operation.
func parseMergeTreeOutput(r io.Reader) (_ []*mergeTreeOutput, retErr error) {
	scan := bufio.NewScanner(r)
	scan.Split(scanutil.SplitNull)
	var (
		current *mergeTreeOutput
		outputs []*mergeTreeOutput
	)
	defer func() {
		if err := scan.Err(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("scan: %w", err))
		}
	}()
	for scan.Scan() && len(scan.Bytes()) > 0 {
		// With --stdin flag, output is always preceded by
		// a "merge status section in the form:
		//   Merge status
		//       This is an integer status followed by a NUL character. The integer status is:
		//           0: merge had conflicts
		//           1: merge was clean
		var clean bool
		switch tok := scan.Text(); tok {
		case "0":
			clean = false
		case "1":
			clean = true
		default:
			return outputs, fmt.Errorf("expected '0' or '1', got %q", tok)
		}

		// Next token is always OID of tree.
		if !scan.Scan() {
			return outputs, errors.New("expected OID of tree, got EOF")
		}

		current = &mergeTreeOutput{TreeHash: Hash(scan.Text())}
		outputs = append(outputs, current)
		if clean {
			// If the merge was clean,
			// no more output is expected for this merge.
			continue
		}

		// Otherwise, we expect two more sections:
		//   <Conflicted file info>
		//   <Informational messages>
		// Because we use --name-only,
		// conflicted file info contains just the file names.
		// Empty file name marks end of that section.
		for scan.Scan() && len(scan.Bytes()) > 0 {
			// TODO: Drop --name-only above
			// and also parse mode, object, and stage of each file.
			current.ConflictFiles = append(current.ConflictFiles, scan.Text())
		}

		// Informational messages are in the form:
		//
		//    <paths> <conflict-type> NUL <conflict-message> NUL
		//
		// Where:
		//
		//    paths = <N:int> NUL <path1> NUL <path2> NUL ... <pathN> NUL
		//    conflict-type = [set of stable strings], including "Auto-merging"
		//    conflict-message = [unstable informational strings]
		//
		// An empty token indicates end of conflict information.
		for scan.Scan() && len(scan.Bytes()) > 0 {
			numPaths, err := strconv.Atoi(scan.Text())
			if err != nil {
				return outputs, fmt.Errorf("expected <number-of-paths>, got %q", scan.Text())
			}

			paths := make([]string, 0, numPaths)
			for idx := range numPaths {
				if !scan.Scan() {
					return outputs, fmt.Errorf("expected path #%d, got EOF", idx+1)
				}
				paths = append(paths, scan.Text())
			}

			if !scan.Scan() {
				return outputs, errors.New("expected <conflict-type>, got EOF")
			}
			conflictType := scan.Text()

			if !scan.Scan() {
				return outputs, errors.New("expected <conflict-message>, got EOF")
			}
			msg := scan.Text()

			current.ConflictMessages = append(current.ConflictMessages, MergeTreeConflictDetails{
				Type:    conflictType,
				Message: msg,
				Paths:   paths,
			})
		}
	}

	return outputs, nil
}

package spice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/xec"
)

// ErrStackEditAborted is returned when the user requests
// for a stack edit operation to be aborted.
var ErrStackEditAborted = errors.New("stack edit aborted")

// StackEditRequest is a request to edit the order of a stack of branches.
type StackEditRequest struct {
	// Stack of branches to edit, with branch closest to trunk first.
	// The first branch in the list will be merged into trunk first.
	Stack []string

	// Editor to use for editing the stack.
	Editor string
}

// StackEditResult is the result of a stack edit operation.
type StackEditResult struct {
	// Stack is the new order of branches after the edit operation.
	// The branch closest to trunk is first in the list.
	Stack []string
}

// StackEdit allows the user to edit the order of branches in a stack.
// The user is presented with an editor containing the list of branches.
//
// Returns [ErrStackEditAborted] if thee operation is aborted by the user.
func (s *Service) StackEdit(ctx context.Context, req *StackEditRequest) (*StackEditResult, error) {
	must.NotBeEmptyf(req.Stack, "stack cannot be empty")
	must.NotContainf(req.Stack, s.store.Trunk(), "cannot edit trunk")
	must.NotBeBlankf(req.Editor, "editor is required")

	// TODO: assert that req.Stack[0] has trunk as its base.
	bottomName := req.Stack[0]
	bottom, err := s.LookupBranch(ctx, req.Stack[0])
	if err != nil {
		return nil, fmt.Errorf("look up lowest branch (%q): %w", req.Stack[0], err)
	}

	branches, err := editStackFile(req.Editor, req.Stack)
	if err != nil {
		return nil, err
	}

	base := bottom.Base
	for idx, branch := range branches {
		req := BranchOntoRequest{
			Branch: branch,
			Onto:   base,
		}

		if len(bottom.MergedDownstack) > 0 {
			// If the bottom-most branch is changing,
			// copy the merged downstack over to it.
			if idx == 0 && branch != bottomName {
				req.MergedDownstack = &bottom.MergedDownstack
			}

			// Also in that case, make sure to clear it
			// from the new position of the original bottom branch.
			if idx > 0 && branch == bottomName {
				var newHistory []json.RawMessage
				req.MergedDownstack = &newHistory
			}
		}

		if err := s.BranchOnto(ctx, &req); err != nil {
			return nil, fmt.Errorf("branch %v onto %v: %w", branch, base, err)
		}
		base = branch
	}

	return &StackEditResult{Stack: branches}, nil
}

// editStackFile opens the editor with the given branches
// and returns the edited branches.
//
// Branches are presented in the reverse order of the input list,
// with the branch closest to trunk at the bottom.
// The response list will be in the same order as the input list.
//
// Returns ErrStackEditAborted if the user aborts the edit operation.
func editStackFile(editor string, branches []string) ([]string, error) {
	originals := make(map[string]struct{}, len(branches))
	for _, branch := range branches {
		originals[branch] = struct{}{}
	}

	branchesFile, err := createStackEditFile(branches)
	if err != nil {
		return nil, err
	}

	editCmd := xec.EditCommand(editor, branchesFile)
	if err := editCmd.Run(); err != nil {
		return nil, fmt.Errorf("run editor: %w", err)
	}

	f, err := os.Open(branchesFile)
	if err != nil {
		return nil, fmt.Errorf("open edited file: %w", err)
	}

	newOrder := make([]string, 0, len(branches))
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		bs := bytes.TrimSpace(scanner.Bytes())
		if len(bs) == 0 || bs[0] == '#' {
			continue
		}

		name := string(bs)
		if _, ok := originals[name]; !ok {
			// TODO: maybe present a better error message
			return nil, fmt.Errorf("branch %q not in the original list, or is duplicated", name)
		}
		delete(originals, name)

		newOrder = append(newOrder, name)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read edited file: %w", err)
	}

	// If the user deleted all lines in the file, abort the operation.
	if len(newOrder) == 0 {
		return nil, ErrStackEditAborted
	}

	slices.Reverse(newOrder)
	return newOrder, nil
}

const _stackEditFileFooter = `
# Edit the order of branches by modifying the list above.
# The branch at the bottom of the list will be merged into trunk first.
# Branches above that will be stacked on top of it in the order they appear.
# Branches deleted from the list will not be modified.
#
# Save and quit the editor to apply the changes.
# Delete all lines in the editor to abort the operation.
`

func createStackEditFile(branches []string) (_ string, err error) {
	// TODO:
	// Is there a file format that'll get highlighted correctly in editors?
	file, err := os.CreateTemp("", "spice-edit-*.txt")
	if err != nil {
		return "", fmt.Errorf("create temporary file: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()

	for i := len(branches) - 1; i >= 0; i-- {
		if _, err := fmt.Fprintln(file, branches[i]); err != nil {
			return "", fmt.Errorf("write branc: %w", err)
		}
	}

	if _, err := io.WriteString(file, _stackEditFileFooter); err != nil {
		return "", fmt.Errorf("write footer: %w", err)
	}

	return file.Name(), nil
}

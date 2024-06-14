package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/text"
)

type downstackEditCmd struct {
	Editor string `env:"EDITOR" help:"Editor to use for editing the downstack."`

	Name string `arg:"" optional:"" help:"Name of the branch to start editing from." predictor:"trackedBranches"`
}

func (*downstackEditCmd) Help() string {
	return text.Dedent(`
		Opens an editor to allow changing the order of branches
		from trunk to the current branch.
		The branch at the top of the list will be checked out
		as the topmost branch in the downstack.
		Branches upstack of the current branch will not be modified.
		Branches deleted from the list will also not be modified.
	`)
}

func (cmd *downstackEditCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, store, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

	if cmd.Editor == "" {
		return errors.New("an editor is required: use --editor or set $EDITOR")
	}

	if cmd.Name == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Name = currentBranch
	}

	if cmd.Name == store.Trunk() {
		return errors.New("cannot edit below trunk")
	}

	downstacks, err := svc.ListDownstack(ctx, cmd.Name)
	if err != nil {
		return fmt.Errorf("list downstack: %w", err)
	}
	must.NotBeEmptyf(downstacks, "downstack cannot be empty")
	must.BeEqualf(downstacks[0], cmd.Name,
		"downstack must start with the original branch")

	if len(downstacks) == 1 {
		log.Infof("nothing to edit below %s", cmd.Name)
		return nil
	}

	originalBranches := make(map[string]struct{}, len(downstacks))
	for _, branch := range downstacks {
		originalBranches[branch] = struct{}{}
	}

	instructionFile, err := createEditFile(downstacks)
	if err != nil {
		return err
	}

	editCmd := exec.CommandContext(ctx, cmd.Editor, instructionFile)
	editCmd.Stdin = os.Stdin
	editCmd.Stdout = os.Stdout
	editCmd.Stderr = os.Stderr
	if err := editCmd.Run(); err != nil {
		return fmt.Errorf("run editor: %w", err)
	}

	f, err := os.Open(instructionFile)
	if err != nil {
		return fmt.Errorf("open edited file: %w", err)
	}

	newOrder := make([]string, 0, len(downstacks))
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		bs := bytes.TrimSpace(scanner.Bytes())
		if len(bs) == 0 || bs[0] == '#' {
			continue
		}

		name := string(bs)
		if _, ok := originalBranches[name]; !ok {
			// TODO: better error
			return fmt.Errorf("branch %q not in original downstack, or is duplicated", name)
		}
		delete(originalBranches, name)

		newOrder = append(newOrder, name)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read edited file: %w", err)
	}

	if len(newOrder) == 0 {
		log.Infof("downstack edit aborted or nothing to do")
		return nil
	}
	newTop := newOrder[0]
	slices.Reverse(newOrder)

	base := store.Trunk()
	for _, branch := range newOrder {
		err := (&branchOntoCmd{
			Branch: branch,
			Onto:   base,
		}).Run(ctx, log, opts)
		if err != nil {
			return fmt.Errorf("branch onto %s: %w", branch, err)
		}
		base = branch
	}

	return (&branchCheckoutCmd{
		Name: newTop,
	}).Run(ctx, log, opts)
}

var _editFooter = `
# Edit the order of branches by modifying the list above.
# The branch at the bottom of the list will be merged into trunk first.
# Branches above that will be stacked on top of it in the order they appear.
# Branches deleted from the list will not be modified.
#
# Save and quit the editor to apply the changes.
# Delete all lines in the editor to abort the operation.
`

func createEditFile(branches []string) (_ string, err error) {
	file, err := os.CreateTemp("", "spice-edit-*.txt")
	if err != nil {
		return "", fmt.Errorf("create temporary file: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()

	for _, branch := range branches {
		if _, err := fmt.Fprintln(file, branch); err != nil {
			return "", fmt.Errorf("write branc: %w", err)
		}
	}

	if _, err := io.WriteString(file, _editFooter); err != nil {
		return "", fmt.Errorf("write footer: %w", err)
	}

	return file.Name(), nil
}

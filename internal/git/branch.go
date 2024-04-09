package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
)

// LocalBranches lists local branches in the repository.
func (r *Repository) LocalBranches(ctx context.Context) ([]string, error) {
	cmd := r.gitCmd(ctx, "branch")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git branch: %w", err)
	}

	if err := cmd.Start(r.exec); err != nil {
		return nil, fmt.Errorf("start git branch: %w", err)
	}

	var branches []string
	scan := bufio.NewScanner(out)
	for scan.Scan() {
		line := bytes.TrimSpace(scan.Bytes())
		if len(line) == 0 {
			continue
		}

		switch line[0] {
		case '(':
			continue // (HEAD detached at ...)
		case '*', '+':
			// Current or checked out in another worktree.
			b := bytes.TrimSpace(line[1:])
			// TODO: instead of returning string,
			// return a list of LocalBranch objects
			// that also specify whether the branch is checked out.
			branches = append(branches, string(b))
		default:
			branches = append(branches, string(line))
		}
	}

	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("read output: %w", err)
	}

	if err := cmd.Wait(r.exec); err != nil {
		return nil, fmt.Errorf("git branch: %w", err)
	}

	return branches, nil
}

func (r *Repository) CurrentBranch(ctx context.Context) (string, error) {
	name, err := r.gitCmd(ctx, "branch", "--show-current").
		OutputString(r.exec)
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return name, nil
}

type CreateBranchRequest struct {
	Name string
	Head string // optional
}

func (r *Repository) CreateBranch(ctx context.Context, req CreateBranchRequest) error {
	args := []string{"branch", req.Name}
	if req.Head != "" {
		args = append(args, req.Head)
	}
	if err := r.gitCmd(ctx, args...).Run(r.exec); err != nil {
		return fmt.Errorf("git branch: %w", err)
	}
	return nil
}

// TODO: combine with Checkout
func (r *Repository) DetachHead(ctx context.Context, commitish string) error {
	args := []string{"checkout", "--detach"}
	if len(commitish) > 0 {
		args = append(args, commitish)
	}
	if err := r.gitCmd(ctx, args...).Run(r.exec); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	return nil
}

func (r *Repository) Checkout(ctx context.Context, branch string) error {
	if err := r.gitCmd(ctx, "checkout", branch).Run(r.exec); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	return nil
}

type BranchDeleteOptions struct {
	Force bool
}

func (r *Repository) DeleteBranch(ctx context.Context, branch string, opts BranchDeleteOptions) error {
	args := []string{"branch", "--delete"}
	if opts.Force {
		args = append(args, "--force")
	}
	args = append(args, branch)

	if err := r.gitCmd(ctx, args...).Run(r.exec); err != nil {
		return fmt.Errorf("git branch: %w", err)
	}
	return nil
}

type RenameBranchRequest struct {
	OldName string
	NewName string
	Force   bool
}

func (r *Repository) RenameBranch(ctx context.Context, req RenameBranchRequest) error {
	args := []string{"branch", "--move"}
	if req.Force {
		args = append(args, "--force")
	}
	args = append(args, req.OldName, req.NewName)

	if err := r.gitCmd(ctx, args...).Run(r.exec); err != nil {
		return fmt.Errorf("git branch: %w", err)
	}
	return nil
}

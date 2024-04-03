package git

import (
	"context"
	"fmt"
)

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
func (r *Repository) DetachHead(ctx context.Context) error {
	if err := r.gitCmd(ctx, "checkout", "--detach").Run(r.exec); err != nil {
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
	args := []string{"branch"}
	if opts.Force {
		args = append(args, "-D")
	} else {
		args = append(args, "-d")
	}
	args = append(args, branch)

	if err := r.gitCmd(ctx, args...).Run(r.exec); err != nil {
		return fmt.Errorf("git branch: %w", err)
	}
	return nil
}

package git

import (
	"context"
	"fmt"
	"os"
)

type RebaseRequest struct {
	Interactive bool
	Onto        string
	Upstream    string
	Branch      string
}

func (r *Repository) Rebase(ctx context.Context, opts RebaseRequest) error {
	args := []string{"rebase"}
	if opts.Interactive {
		args = append(args, "--interactive")
	}
	if opts.Onto != "" {
		args = append(args, "--onto", opts.Onto)
	}
	if opts.Upstream != "" {
		args = append(args, opts.Upstream)
	}
	if opts.Branch != "" {
		args = append(args, opts.Branch)
	}

	err := r.gitCmd(ctx, args...).
		Stdin(os.Stdin).
		Stdout(os.Stdout).
		Stderr(os.Stderr).
		Run(r.exec)
	if err != nil {
		return fmt.Errorf("rebase: %w", err)
	}

	return nil
}

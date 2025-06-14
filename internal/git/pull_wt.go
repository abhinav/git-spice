package git

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/silog"
)

// PullOptions specifies options for the Pull operation.
type PullOptions struct {
	Remote    string
	Rebase    bool
	Autostash bool
	Refspec   Refspec
}

// Pull fetches objects and refs from a remote repository
// and merges them into the current branch.
func (w *Worktree) Pull(ctx context.Context, opts PullOptions) error {
	if opts.Refspec != "" && opts.Remote == "" {
		return errors.New("refspec specified without remote")
	}

	w.log.Debug("Pulling from remote", silog.NonZero("name", opts.Remote))

	args := []string{"pull"}
	if opts.Rebase {
		args = append(args, "--rebase")
	}
	if opts.Autostash {
		args = append(args, "--autostash")
	}
	if opts.Remote != "" {
		args = append(args, opts.Remote)
	}
	if opts.Refspec != "" {
		args = append(args, opts.Refspec.String())
	}

	if err := w.gitCmd(ctx, args...).Run(w.exec); err != nil {
		return fmt.Errorf("git pull: %w", err)
	}

	return nil
}

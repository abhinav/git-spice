package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"go.abhg.dev/gs/internal/silog"
)

// PushOptions specifies options for the Push operation.
type PushOptions struct {
	// Remote is the remote to push to.
	//
	// If empty, the default remote for the current branch is used.
	// If the current branch does not have a remote configured,
	// the operation fails.
	Remote string

	// Force indicates that a push should overwrite the ref.
	Force bool

	// ForceWithLease indicates that a push should overwrite a ref
	// even if the new value is not a descendant of the current value
	// provided that our knowledge of the current value is up-to-date.
	ForceWithLease string

	// Refspec is the refspec to push.
	// If empty, the current branch is pushed to the remote.
	Refspec Refspec

	// NoVerify indicates that pre-push hooks should be bypassed.
	NoVerify bool
}

// Push pushes objects and refs to a remote repository.
func (w *Worktree) Push(ctx context.Context, opts PushOptions) error {
	if opts.Remote == "" && opts.Refspec == "" {
		return errors.New("push: no remote or refspec specified")
	}

	w.log.Debug("Pushing to remote",
		silog.NonZero("name", opts.Remote),
		silog.NonZero("force", opts.Force),
		silog.NonZero("lease", forceWithLease(opts.ForceWithLease)))

	args := []string{"push"}
	if lease := opts.ForceWithLease; lease != "" {
		args = append(args, "--force-with-lease="+lease)
	}
	if opts.Force {
		args = append(args, "--force")
	}
	if opts.NoVerify {
		args = append(args, "--no-verify")
	}
	if opts.Remote != "" {
		args = append(args, opts.Remote)
	}
	if opts.Refspec != "" {
		args = append(args, opts.Refspec.String())
	}

	cmd := w.gitCmd(ctx, args...).CaptureStdout()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("push: %w", err)
	}

	return nil
}

type forceWithLease string

func (f forceWithLease) String() string {
	return string(f)
}

func (f forceWithLease) LogValue() slog.Value {
	ref, hash, ok := strings.Cut(string(f), ":")
	if !ok {
		return slog.StringValue(string(f))
	}

	return slog.GroupValue(
		slog.String("ref", ref),
		slog.String("hash", Hash(hash).Short()),
	)
}

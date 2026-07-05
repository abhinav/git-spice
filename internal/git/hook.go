package git

import (
	"cmp"
	"context"
	"fmt"
)

// HookRunOptions configures hook execution.
type HookRunOptions struct {
	// Args contains arguments to pass to the hook after "--".
	Args []string

	// Env contains environment assignments
	// to add for the hook process.
	Env []string
}

// HookRun runs the named Git hook with the given arguments.
// If the hook does not exist, it returns nil.
// If the hook exits non-zero, it returns an error.
func (r *Repository) HookRun(
	ctx context.Context,
	hook string,
	opts *HookRunOptions,
) error {
	opts = cmp.Or(opts, &HookRunOptions{})

	cmdArgs := make([]string, 0, 4+len(opts.Args))
	cmdArgs = append(cmdArgs, "hook", "run", "--ignore-missing", hook)
	if len(opts.Args) > 0 {
		cmdArgs = append(cmdArgs, "--")
		cmdArgs = append(cmdArgs, opts.Args...)
	}

	cmd := r.gitCmd(ctx, cmdArgs[0], cmdArgs[1:]...)
	if len(opts.Env) > 0 {
		cmd = cmd.AppendEnv(opts.Env...)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook run %q: %w", hook, err)
	}
	return nil
}

package git

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"go.abhg.dev/gs/internal/must"
)

// ResetMode specifies the reset mode used in the form:
//
//	git reset --<mode> <commit>
//
// The default mode is ResetMixed.
type ResetMode int

const (
	// ResetModeUnset is the default reset mode.
	ResetModeUnset ResetMode = iota

	// ResetMixed resets the index to the specified commit
	// but leaves the working tree unchanged.
	ResetMixed

	// ResetHard resets the index and working tree to the specified commit.
	ResetHard

	// ResetSoft resets HEAD to the specified commit,
	// leaving the index and working tree unchanged.
	ResetSoft
)

func (m ResetMode) String() string {
	switch m {
	case ResetMixed:
		return "mixed"
	case ResetHard:
		return "hard"
	case ResetSoft:
		return "soft"
	case ResetModeUnset:
		return "unset"
	default:
		return strconv.Itoa(int(m))
	}
}

// ResetOptions configures the behavior of Reset.
type ResetOptions struct {
	Quiet bool
	Mode  ResetMode

	// Patch lets the user choose which hunks to stage.
	// Mode must be ResetModeUnset.
	Patch bool

	// Update the index entries for the specified paths only.
	// Leave the working tree and the current branch unchanged.
	// Mode must be ResetModeUnset.
	Paths []string
}

// Reset resets the index and optionally the working tree
// to the specified commit.
func (w *Worktree) Reset(ctx context.Context, commit string, opts ResetOptions) error {
	args := []string{"reset"}
	if opts.Quiet {
		args = append(args, "--quiet")
	}
	if opts.Patch {
		must.BeEqualf(opts.Mode, ResetModeUnset, "patch mode requires mixed reset mode")
		args = append(args, "--patch")
	}
	switch opts.Mode {
	case ResetModeUnset:
		// use default
	case ResetMixed:
		args = append(args, "--mixed")
	case ResetHard:
		args = append(args, "--hard")
	case ResetSoft:
		args = append(args, "--soft")
	default:
		must.Failf("unknown reset mode: %d", opts.Mode)
	}

	args = append(args, commit)
	if len(opts.Paths) > 0 {
		must.BeEqualf(opts.Mode, ResetModeUnset, "resetting paths requires mixed reset mode")
		args = append(args, "--")
		args = append(args, opts.Paths...)

		w.log.Debug("Resetting paths", "commit", commit, "paths", opts.Paths)
	} else {
		w.log.Debug("Resetting repository", "commit", commit, "mode", opts.Mode)
	}

	cmd := w.gitCmd(ctx, args...)
	if opts.Patch {
		cmd.Stdin(os.Stdin)
		cmd.Stdout(os.Stdout)
	}
	if err := cmd.Run(w.exec); err != nil {
		return fmt.Errorf("git reset: %w", err)
	}

	return nil
}

package git

import (
	"context"
	"fmt"
	"os"
)

// CommitRequest is a request to commit changes.
// It relies on the 'git commit' command.
type CommitRequest struct {
	// Message is the commit message.
	//
	// If this and ReuseMessag are empty,
	// $EDITOR is opened to edit the message.
	Message string

	// ReuseMessage uses the commit message from the given commitish
	// as the commit message.
	ReuseMessage string

	// Template is the commit message template.
	//
	// If Message is empty, this fills the initial commit message
	// when the user is editing the commit message.
	//
	// Note that if the user does not edit the message,
	// the commit will be aborted.
	// Therefore, do not use this as a default message.
	Template string

	// All stages all changes before committing.
	All bool

	// Amend amends the last commit.
	Amend bool

	// NoEdit skips editing the commit message.
	NoEdit bool

	// AllowEmpty allows a commit with no changes.
	AllowEmpty bool

	// Create a new commit which "fixes up" the commit at the given commitish.
	Fixup string

	// NoVerify allows a commit with pre-commit and commit-msg hooks bypassed.
	NoVerify bool

	// Signoff adds a Signed-off-by trailer to the commit message.
	Signoff bool

	// If set, the Author and/or Committer signatures are used for the commit.
	Author, Committer *Signature
}

// Commit runs the 'git commit' command,
// allowing the user to commit changes.
func (w *Worktree) Commit(ctx context.Context, req CommitRequest) error {
	args := []string{"commit"}
	if req.All {
		args = append(args, "-a")
	}
	if req.Message != "" {
		args = append(args, "-m", req.Message)
	}
	if req.Template != "" {
		f, err := os.CreateTemp("", "commit-template-")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		defer func() { _ = os.Remove(f.Name()) }()

		if _, err := f.WriteString(req.Template); err != nil {
			return fmt.Errorf("write temp file: %w", err)
		}

		if err := f.Close(); err != nil {
			return fmt.Errorf("close temp file: %w", err)
		}

		args = append(args, "--template", f.Name())
	}
	if req.Amend {
		args = append(args, "--amend")
	}
	if req.NoEdit {
		args = append(args, "--no-edit")
	}
	if req.AllowEmpty {
		args = append(args, "--allow-empty")
	}
	if req.NoVerify {
		args = append(args, "--no-verify")
	}
	if req.ReuseMessage != "" {
		args = append(args, "-C", req.ReuseMessage)
	}
	if req.Fixup != "" {
		args = append(args, "--fixup", req.Fixup)
	}
	if req.Signoff {
		args = append(args, "--signoff")
	}

	cmd := w.gitCmd(ctx, args...).
		WithStdin(os.Stdin).
		WithStdout(os.Stdout).
		WithStderr(os.Stderr)
	if req.Author != nil {
		cmd.AppendEnv(req.Author.appendEnv("AUTHOR", nil)...)
	}
	if req.Committer != nil {
		cmd.AppendEnv(req.Committer.appendEnv("COMMITTER", nil)...)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

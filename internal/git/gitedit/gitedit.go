// Package gitedit coordinates editing of commit messages
// using the user's Git editor.
//
// It replicates the editor UX of git commit:
// invoking $(git var GIT_EDITOR) with a COMMIT_EDITMSG file,
// comments based on core.commentChar/core.commentString,
// optional verbose diff, message cleanup, and hook invocation.
package gitedit

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/sigstack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
)

// lastByteWriter wraps an io.Writer,
// tracking the last byte written.
type lastByteWriter struct {
	w    io.Writer
	last byte
	seen bool
}

func (w *lastByteWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if n > 0 {
		w.last = p[n-1]
		w.seen = true
	}
	return n, err
}

// nonSpaceCounter wraps an io.Writer,
// counting non-whitespace bytes written.
type nonSpaceCounter struct {
	w     io.Writer
	count int
}

func (c *nonSpaceCounter) Write(p []byte) (int, error) {
	for _, b := range p {
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			c.count++
		}
	}
	return c.w.Write(p)
}

// Repository defines the Git operations needed by the editor.
type Repository interface {
	// Var returns the value of a Git variable.
	Var(ctx context.Context, name string) (string, error)

	// DiffTreePatch writes the patch diff between two tree-ish
	// references to w.
	DiffTreePatch(ctx context.Context, w io.Writer,
		treeish1, treeish2 string) error

	// Stripspace processes input through git stripspace,
	// writing the result to w.
	Stripspace(ctx context.Context, r io.Reader, w io.Writer,
		opts *git.StripspaceOptions) error

	// HookRun runs the named Git hook with the given arguments.
	// Returns nil if the hook does not exist or exits 0.
	HookRun(ctx context.Context, hook string,
		opts *git.HookRunOptions) error
}

var _ Repository = (*git.Repository)(nil)

// SignalStack manages signal handler registration
// while the editor is running.
// [sigstack.Stack] satisfies this interface.
type SignalStack interface {
	// Notify registers ch to receive the given signals.
	Notify(ch chan<- os.Signal, sigs ...os.Signal)

	// Stop unregisters ch from all signals.
	Stop(ch chan<- os.Signal)
}

var _ SignalStack = (*sigstack.Stack)(nil)

// Editor invokes the user's Git editor
// to edit commit messages.
type Editor struct {
	// Repository provides access to Git operations.
	Repository Repository // required

	// Signals is the signal handler stack
	// used to manage signal handling
	// while the editor is running.
	Signals SignalStack // required

	// Log is the logger.
	Log *silog.Logger // required

	// CommentString is the string used to mark comment lines.
	// This is the value of core.commentString
	// or core.commentChar in the Git configuration.
	// Defaults to "#" if empty.
	CommentString string

	// CleanupMode is the commit message cleanup mode.
	// This is the value of commit.cleanup.
	// Defaults to "strip" if empty.
	CleanupMode string

	// Verbose specifies whether committing in verbose mode.
	// If true, a diff of the changes being committed is included
	// in the editor.
	Verbose bool
}

func (e *Editor) commentString() string {
	return cmp.Or(e.CommentString, "#")
}

func (e *Editor) cleanupMode() string {
	return cmp.Or(e.CleanupMode, "strip")
}

// EditCommitMessageOptions specifies options for EditCommitMessage.
type EditCommitMessageOptions struct {
	// Env contains environment assignments
	// appended to the current environment
	// for hooks and the editor.
	//
	// Nil and empty both mean no additional environment.
	Env []string

	// NoVerify, if true, indicates that the following hooks
	// should be skipped:
	//
	//  - commit-msg
	NoVerify bool

	// Commit is the hash of the commit being edited.
	// Used for the prepare-commit-msg hook argument
	// and for generating the verbose diff.
	Commit git.Hash

	// Parent is the parent commit hash.
	// Required if Verbose is true to generate the diff.
	// The diff shown is Parent..Commit.
	Parent git.Hash
}

// EditCommitMessage opens the user's editor
// to edit a commit message,
// writing the cleaned result to dst.
//
// It writes a temporary COMMIT_EDITMSG file,
// runs the prepare-commit-msg and commit-msg hooks,
// and streams the cleaned commit message to dst.
func (e *Editor) EditCommitMessage(
	ctx context.Context,
	src io.Reader,
	dst io.Writer,
	opts *EditCommitMessageOptions,
) error {
	opts = cmp.Or(opts, &EditCommitMessageOptions{})

	// Git resolves the editor with git_editor() in editor.c,
	// which checks GIT_EDITOR, core.editor, VISUAL, EDITOR,
	// and falls back to a compiled-in default.
	// "git var GIT_EDITOR" performs all of that resolution for us.
	// https://github.com/git/git/blob/f0ef5b6d9bcc258e4cbef93839d1b7465d5212b9/editor.c#L27
	editorCmd, err := e.Repository.Var(ctx, "GIT_EDITOR")
	if err != nil {
		return fmt.Errorf("resolve editor: %w", err)
	}

	// Git uses .git/COMMIT_EDITMSG,
	// but we'll use a temporary file
	// to avoid issues with concurrent edits and to ensure cleanup.
	tmpDir, err := os.MkdirTemp("", "gitedit")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	msgPath := filepath.Join(tmpDir, "COMMIT_EDITMSG")
	if err := e.writeCommitEditMsg(ctx, src, msgPath, opts); err != nil {
		return fmt.Errorf("write COMMIT_EDITMSG: %w", err)
	}

	// prepare-commit-msg hook accepts 1-3 parameters:
	//     <file> [source]
	//     <file> "commit" [hash]
	// Where [source] is
	// "message" (-m/-F), "template" (-t), "merge", "squash", or "commit".
	// If the source is "commit", then [hash] is the commit under question
	// (-c, -C, or --amend).
	//
	// NB: prepare-commit-msg is not skipped by --no-verify.
	// https://github.com/git/git/blob/f0ef5b6d9bcc258e4cbef93839d1b7465d5212b9/builtin/commit.c#L1115-L1116
	hookArgs := []string{msgPath}
	if opts.Commit != "" {
		hookArgs = append(hookArgs, "commit", opts.Commit.String())
	}
	if err := e.Repository.HookRun(ctx, "prepare-commit-msg", &git.HookRunOptions{
		Args: hookArgs,
		Env:  opts.Env,
	}); err != nil {
		return fmt.Errorf("prepare-commit-msg: %w", err)
	}

	// Git ignores SIGINT and SIGQUIT while the editor is running.
	// We'll do the same.
	// https://github.com/git/git/blob/f0ef5b6d9bcc258e4cbef93839d1b7465d5212b9/editor.c#L100-L101
	sigc := make(chan os.Signal, 1)
	err = func() error {
		e.Signals.Notify(sigc, syscall.SIGINT, syscall.SIGQUIT)
		cmd := xec.EditCommand(editorCmd, msgPath)
		cmd.Env = append(cmd.Env, opts.Env...)
		err := cmd.Run()
		e.Signals.Stop(sigc)
		return err
	}()
	if err != nil {
		return fmt.Errorf("editor %q: %w", editorCmd, err)
	}

	// usage: commit-msg <file>
	// Can be skipped with --no-verify.
	// https://github.com/git/git/blob/f0ef5b6d9bcc258e4cbef93839d1b7465d5212b9/builtin/commit.c#L1131-L1133
	if !opts.NoVerify {
		if err := e.Repository.HookRun(ctx, "commit-msg", &git.HookRunOptions{
			Args: []string{msgPath},
			Env:  opts.Env,
		}); err != nil {
			return fmt.Errorf("commit-msg: %w", err)
		}
	}

	counter := &nonSpaceCounter{w: dst}
	if err := e.cleanupMessage(ctx, msgPath, counter); err != nil {
		return fmt.Errorf("cleanup message: %w", err)
	}

	if counter.count == 0 {
		return errors.New("empty commit message")
	}

	return nil
}

// writeCommitEditMsg builds the COMMIT_EDITMSG file.
//
// The structure mirrors prepare_to_commit() in Git's builtin/commit.c,
// which writes the message, appends instructions, and optionally a diff.
// https://github.com/git/git/blob/f0ef5b6d9bcc258e4cbef93839d1b7465d5212b9/builtin/commit.c#L761
func (e *Editor) writeCommitEditMsg(
	ctx context.Context,
	src io.Reader,
	path string,
	opts *EditCommitMessageOptions,
) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	// Original message.
	// Track the last byte written
	// to ensure the message ends with a newline
	// before appending instructions.
	lbw := &lastByteWriter{w: f}
	if _, err := io.Copy(lbw, src); err != nil {
		return err
	}
	if !lbw.seen || lbw.last != '\n' {
		if _, err := io.WriteString(f, "\n"); err != nil {
			return err
		}
	}

	if err := e.writeInstructions(ctx, f); err != nil {
		return fmt.Errorf("write instructions: %w", err)
	}

	// If verbose, and there's sufficient information, generate a diff.
	if e.Verbose && opts.Commit != "" && opts.Parent != "" {
		if err := e.writeVerboseDiff(ctx, f, opts.Parent, opts.Commit); err != nil {
			// Proceed even if this fails.
			e.Log.Warn("Could not write diff to COMMIT_EDITMSG", "error", err)
		}
	}

	return f.Close()
}

// writeInstructions appends commented instructions
// to the COMMIT_EDITMSG file based on the cleanup mode.
//
// The instruction text matches Git's hint_cleanup_all
// and hint_cleanup_space strings.
// https://github.com/git/git/blob/f0ef5b6d9bcc258e4cbef93839d1b7465d5212b9/builtin/commit.c#L945-L960
func (e *Editor) writeInstructions(
	ctx context.Context,
	w io.Writer,
) error {
	if _, err := io.WriteString(w, "\n"); err != nil {
		return err
	}

	var instructions string
	switch e.cleanupMode() {
	case "scissors":
		// Scissors mode: no extra instructions.
		// The scissors line is added in writeVerboseDiff
		// or as a standalone marker.
		return nil

	case "whitespace":
		instructions = "Please enter the commit message for your changes.\n" +
			"Lines starting with '" + e.commentString() + "' will be kept;\n" +
			"you may remove them yourself if you want to.\n" +
			"An empty message aborts the commit.\n"

	default: // "strip" and others
		instructions = "Please enter the commit message for your changes.\n" +
			"Lines starting with '" + e.commentString() + "' will be ignored,\n" +
			"and an empty message aborts the commit.\n"
	}

	// Use git stripspace --comment-lines
	// to prepend the comment string.
	return e.Repository.Stripspace(ctx, strings.NewReader(instructions), w, &git.StripspaceOptions{
		CommentLines: true,
	})
}

// _cutLine is the scissors line content,
// matching Git's cut_line constant in wt-status.c.
// Note: Git's constant includes a trailing newline; ours does not
// because isScissorsLine strips the newline before comparing.
// https://github.com/git/git/blob/f0ef5b6d9bcc258e4cbef93839d1b7465d5212b9/wt-status.c#L42-L43
const _cutLine = "------------------------ >8 ------------------------"

var _cutLineBytes = []byte(_cutLine)

// writeVerboseDiff appends a scissors line and the diff
// for the commit being edited.
//
// The scissors line is always inserted before the diff
// regardless of cleanup mode,
// matching Git's wt_longstatus_print_verbose behavior.
// https://github.com/git/git/blob/f0ef5b6d9bcc258e4cbef93839d1b7465d5212b9/wt-status.c#L1137
func (e *Editor) writeVerboseDiff(
	ctx context.Context,
	w io.Writer,
	parent, commit git.Hash,
) error {
	// The scissor line itself.
	if _, err := io.WriteString(w, e.commentString()+" "+_cutLine+"\n"); err != nil {
		return err
	}

	// Instructions about it.
	instructions := "Do not modify or remove the line above.\n" +
		"Everything below it will be ignored.\n"
	if err := e.Repository.Stripspace(
		ctx, strings.NewReader(instructions), w, &git.StripspaceOptions{
			CommentLines: true,
		},
	); err != nil {
		return err
	}

	// Write the diff.
	return e.Repository.DiffTreePatch(ctx, w, parent.String(), commit.String())
}

// cleanupMessage applies the configured cleanup mode
// to the raw message, writing the result to dst.
//
// The cleanup logic mirrors cleanup_message() in Git's sequencer.c:
// verbatim skips all processing, scissors truncates at the cut line
// then stripspaces, whitespace stripspaces without stripping comments,
// and strip (the default) stripspaces with comment stripping.
// https://github.com/git/git/blob/f0ef5b6d9bcc258e4cbef93839d1b7465d5212b9/sequencer.c#L1210
func (e *Editor) cleanupMessage(
	ctx context.Context,
	msgPath string,
	dst io.Writer,
) error {
	f, err := os.Open(msgPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	switch e.cleanupMode() {
	case "verbatim":
		_, err := io.Copy(dst, f)
		return err

	case "scissors":
		// Truncate at the scissors line,
		// then strip whitespace.
		return e.Repository.Stripspace(
			ctx,
			newScissorsReader(f, e.commentString()),
			dst,
			nil,
		)

	case "whitespace":
		return e.Repository.Stripspace(ctx, f, dst, nil)

	default: // "strip"
		return e.Repository.Stripspace(ctx, f, dst, &git.StripspaceOptions{
			StripComments: true,
		})
	}
}

// newScissorsReader returns an io.Reader
// that truncates input at the scissors line.
//
// It streams line-by-line, never holding more than
// a single line's worth of data in memory beyond
// what has already been copied to the caller.
func newScissorsReader(r io.Reader, comment string) io.Reader {
	return newLineReader(r, func(line []byte) bool {
		return isScissorsLine(line, comment)
	})
}

// isScissorsLine reports whether line is a scissors cut line.
//
// This matches the scissors detection logic in wt_status_locate_end,
// which searches for "\n<comment> <cut_line>" in the buffer.
// We replicate the same checks line-by-line:
//  1. Trim trailing whitespace.
//  2. Strip the comment string prefix.
//  3. Trim leading whitespace after the prefix.
//  4. Compare against _cutLineBytes.
//
// https://github.com/git/git/blob/f0ef5b6d9bcc258e4cbef93839d1b7465d5212b9/wt-status.c#L1100
func isScissorsLine(line []byte, comment string) bool {
	line = bytes.TrimRight(line, " \t")
	line = bytes.TrimPrefix(line, []byte(comment))
	line = bytes.TrimLeft(line, " \t")
	return bytes.Equal(line, _cutLineBytes)
}

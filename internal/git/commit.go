package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Signature holds authorship information for a commit.
type Signature struct {
	// Name of the signer.
	Name string

	// Email of the signer.
	Email string

	// Time at which the signature was made.
	// If this is zero, the current time is used.
	Time time.Time
}

// typ is one of "COMMIT" or "AUTHOR".
func (s *Signature) appendEnv(typ string, env []string) []string {
	if s == nil {
		return env
	}

	env = append(env, "GIT_"+typ+"_NAME="+s.Name)
	env = append(env, "GIT_"+typ+"_EMAIL="+s.Email)
	if !s.Time.IsZero() {
		env = append(env, "GIT_"+typ+"_DATE="+s.Time.Format(time.RFC3339))
	}
	return env
}

// CommitTreeRequest is a request to create a new commit.
type CommitTreeRequest struct {
	// Hash is the hash of a tree object
	// representing the state of the repository
	// at the time of the commit.
	Tree Hash // required

	// Message is the commit message.
	Message string // required

	// Parents are the hashes of the parent commits.
	// This will usually have one element.
	// It may have more than one element for a merge commit,
	// and no elements for the initial commit.
	Parents []Hash

	// Author and Committer sign the commit.
	// If Committer is nil, Author is used for both.
	//
	// If both are nil, the current user is used.
	// Note that current user may not be available in all contexts.
	// Prefer to set Author and Committer explicitly.
	Author, Committer *Signature
}

// CommitTree creates a new commit with a given tree hash
// as the state of the repository.
//
// It returns the hash of the new commit.
func (r *Repository) CommitTree(ctx context.Context, req CommitTreeRequest) (Hash, error) {
	if req.Message == "" {
		return ZeroHash, errors.New("empty commit message")
	}
	if req.Committer == nil {
		req.Committer = req.Author
	}

	args := make([]string, 0, 2+2*len(req.Parents))
	args = append(args, "commit-tree")
	for _, parent := range req.Parents {
		args = append(args, "-p", parent.String())
	}
	args = append(args, req.Tree.String())

	var env []string
	env = req.Author.appendEnv("AUTHOR", env)
	env = req.Committer.appendEnv("COMMITTER", env)

	cmd := r.gitCmd(ctx, args...).
		AppendEnv(env...).
		StdinString(req.Message)
	out, err := cmd.OutputString(r.exec)
	if err != nil {
		return ZeroHash, fmt.Errorf("commit-tree: %w", err)
	}

	return Hash(out), nil
}

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
}

// Commit runs the 'git commit' command,
// allowing the user to commit changes.
func (r *Repository) Commit(ctx context.Context, req CommitRequest) error {
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

	err := r.gitCmd(ctx, args...).
		Stdin(os.Stdin).
		Stdout(os.Stdout).
		Stderr(os.Stderr).
		Run(r.exec)
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// CommitSubject returns the subject of a commit.
func (r *Repository) CommitSubject(ctx context.Context, commitish string) (string, error) {
	out, err := r.gitCmd(ctx,
		"show", "--no-patch", "--format=%s", commitish,
	).OutputString(r.exec)
	if err != nil {
		return "", fmt.Errorf("git log: %w", err)
	}
	return out, nil
}

// CommitMessage is the subject and body of a commit.
type CommitMessage struct {
	// Subject for the commit.
	// Contains no leading or trailing whitespace.
	Subject string

	// Body of the commit.
	// Contains no leading or trailing whitespace.
	Body string
}

func (m CommitMessage) String() string {
	if m.Body != "" {
		return m.Subject + "\n\n" + m.Body
	}
	return m.Subject
}

// CommitMessageRange returns the commit messages in the range (start, ^stop).
// That is, all commits reachable from start but not from stop.
func (r *Repository) CommitMessageRange(ctx context.Context, start, stop string) ([]CommitMessage, error) {
	cmd := r.gitCmd(ctx, "rev-list",
		"--format=%B%x00", // null-byte separated
		start, "--not", stop, "--",
	)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}

	if err := cmd.Start(r.exec); err != nil {
		return nil, fmt.Errorf("start rev-list: %w", err)
	}

	scanner := bufio.NewScanner(out)
	scanner.Split(splitNullByte)

	var bodies []CommitMessage
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if len(raw) == 0 {
			continue
		}

		// --format with rev-list writes in the form:
		//
		//	commit <hash>\n
		//	<format string>
		//
		// We need to drop the first line.
		_, raw, _ = strings.Cut(raw, "\n")
		subject, body, _ := strings.Cut(raw, "\n")
		bodies = append(bodies, CommitMessage{
			Subject: strings.TrimSpace(subject),
			Body:    strings.TrimSpace(body),
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	if err := cmd.Wait(r.exec); err != nil {
		return nil, fmt.Errorf("rev-list: %w", err)
	}

	return bodies, nil
}

func splitNullByte(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	if i := bytes.IndexByte(data, 0); i >= 0 {
		// Have a null-byte separated section.
		return i + 1, data[:i], nil
	}

	// No null-byte found, but end of input,
	// so consume the rest as one section.
	if atEOF {
		return len(data), data, nil
	}

	// Request more data.
	return 0, nil, nil
}

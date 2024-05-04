package git

import (
	"context"
	"fmt"
	"os"
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
		return ZeroHash, fmt.Errorf("empty commit message")
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
	// If empty, $EDITOR is opened to edit the message.
	Message string

	// All stages all changes before committing.
	All bool

	// Amend amends the last commit.
	Amend bool

	// NoEdit skips editing the commit message.
	NoEdit bool

	// AllowEmpty allows a commit with no changes.
	AllowEmpty bool
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
	if req.Amend {
		args = append(args, "--amend")
	}
	if req.NoEdit {
		args = append(args, "--no-edit")
	}
	if req.AllowEmpty {
		args = append(args, "--allow-empty")
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

func (r *Repository) CommitSubject(ctx context.Context, commitish string) (string, error) {
	out, err := r.gitCmd(ctx, "log", "-1", "--pretty=format:%s", commitish).
		OutputString(r.exec)
	if err != nil {
		return "", fmt.Errorf("git log: %w", err)
	}
	return out, nil
}

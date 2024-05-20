package git

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/log"
)

// InitOptions configures the behavior of Init.
type InitOptions struct {
	// Log specifies the logger to use for messages.
	Log *log.Logger

	// Branch is the name of the initial branch to create.
	// Defaults to "main".
	Branch string

	exec execer
}

// Init initializes a new Git repository at the given directory.
// If dir is empty, the current working directory is used.
func Init(ctx context.Context, dir string, opts InitOptions) (*Repository, error) {
	if opts.exec == nil {
		opts.exec = _realExec
	}
	if opts.Branch == "" {
		opts.Branch = "main"
	}

	initCmd := newGitCmd(ctx, opts.Log,
		"init",
		"--initial-branch="+opts.Branch,
	).Dir(dir)
	if err := initCmd.Run(opts.exec); err != nil {
		return nil, fmt.Errorf("git init: %w", err)
	}

	return Open(ctx, dir, OpenOptions{
		Log:  opts.Log,
		exec: opts.exec,
	})
}

// OpenOptions configures the behavior of Open.
type OpenOptions struct {
	// Log specifies the logger to use for messages.
	Log *log.Logger

	exec execer
}

// Open opens the repository at the given directory.
// If dir is empty, the current working directory is used.
func Open(ctx context.Context, dir string, opts OpenOptions) (*Repository, error) {
	if opts.exec == nil {
		opts.exec = _realExec
	}
	if opts.Log == nil {
		opts.Log = log.New(io.Discard)
	}

	out, err := newGitCmd(ctx, opts.Log,
		"rev-parse",
		"--show-toplevel",
		"--absolute-git-dir",
	).Dir(dir).OutputString(opts.exec)
	if err != nil {
		return nil, err
	}

	root, gitDir, ok := strings.Cut(out, "\n")
	if !ok {
		return nil, fmt.Errorf("unexpected output from git rev-parse: %q", out)
	}

	return newRepository(root, gitDir, opts.Log, opts.exec), nil
}

// Repository is a handle to a Git repository.
// It provides read-write access to the repository's contents.
type Repository struct {
	root   string
	gitDir string

	log  *log.Logger
	exec execer
}

func newRepository(root, gitDir string, log *log.Logger, exec execer) *Repository {
	return &Repository{
		root:   root,
		gitDir: gitDir,
		log:    log,
		exec:   exec,
	}
}

// gitCmd returns a gitCmd that will run
// with the repository's root as the working directory.
func (r *Repository) gitCmd(ctx context.Context, args ...string) *gitCmd {
	return newGitCmd(ctx, r.log, args...).Dir(r.root)
}

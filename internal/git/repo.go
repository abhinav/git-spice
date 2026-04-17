package git

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/silog"
)

// InitOptions configures the behavior of Init.
type InitOptions struct {
	// Log specifies the logger to use for messages.
	Log *silog.Logger

	// IndexLockTimeout configures how long to spend retrying
	// git commands that fail because the index lock is held.
	// If nil, the package default is used.
	// If zero, retries are disabled.
	IndexLockTimeout *time.Duration

	// Branch is the name of the initial branch to create.
	// Defaults to "main".
	Branch string

	exec execer
}

// Init initializes a new Git repository at the given directory.
func Init(ctx context.Context, dir string, opts InitOptions) (*Repository, *Worktree, error) {
	wt, err := InitWorktree(ctx, dir, opts)
	if err != nil {
		return nil, nil, err
	}

	repo := wt.Repository()
	return repo, wt, nil
}

// InitWorktree initializes a new Git repository at the given directory.
// If dir is empty, the current working directory is used.
func InitWorktree(ctx context.Context, dir string, opts InitOptions) (*Worktree, error) {
	if opts.exec == nil {
		opts.exec = _realExec
	}
	log := opts.Log
	if log == nil {
		log = silog.Nop()
	}
	if opts.Branch == "" {
		opts.Branch = "main"
	}

	commonOpts := commonOptions{
		log:              log,
		exec:             opts.exec,
		indexLockTimeout: _defaultIndexLockTimeout,
	}
	if opts.IndexLockTimeout != nil {
		commonOpts.indexLockTimeout = max(*opts.IndexLockTimeout, 0)
	}

	if opts.Log != nil {
		opts.Log.Debug("Initializing repository", "path", dir)
	}
	initCmd := newGitCmd(ctx, commonOpts.log, commonOpts.exec,
		"init",
		"--initial-branch="+opts.Branch,
	).WithDir(dir)
	if err := initCmd.Run(); err != nil {
		return nil, fmt.Errorf("git init: %w", err)
	}

	return OpenWorktree(ctx, dir, OpenOptions{
		Log:              opts.Log,
		IndexLockTimeout: opts.IndexLockTimeout,
		exec:             opts.exec,
	})
}

// OpenOptions configures the behavior of Open.
type OpenOptions struct {
	// Log specifies the logger to use for messages.
	Log *silog.Logger

	// IndexLockTimeout configures how long to spend retrying
	// git commands that fail because the index lock is held.
	// If nil, the package default is used.
	// If zero, retries are disabled.
	IndexLockTimeout *time.Duration

	exec execer
}

// OpenWorktree opens a worktree of this repository at the given directory,
// automatically detecting the repository's root directory.
func OpenWorktree(ctx context.Context, dir string, opts OpenOptions) (*Worktree, error) {
	if opts.exec == nil {
		opts.exec = _realExec
	}
	log := opts.Log
	if log == nil {
		log = silog.Nop()
	}
	commonOpts := commonOptions{
		log:              log,
		exec:             opts.exec,
		indexLockTimeout: _defaultIndexLockTimeout,
	}
	if opts.IndexLockTimeout != nil {
		commonOpts.indexLockTimeout = max(*opts.IndexLockTimeout, 0)
	}

	out, err := newGitCmd(ctx, commonOpts.log, commonOpts.exec,
		"rev-parse",
		"--path-format=absolute",
		"--show-toplevel",
		"--git-common-dir",
		"--git-dir",
	).WithDir(dir).OutputChomp()
	if err != nil {
		return nil, err
	}

	rootDir, out, ok := strings.Cut(out, "\n")
	if !ok {
		return nil, fmt.Errorf("unexpected output from git rev-parse: %q", out)
	}
	gitCommonDir, gitDir, ok := strings.Cut(out, "\n")
	if !ok {
		return nil, fmt.Errorf("unexpected output from git rev-parse: %q", out)
	}

	repo := newRepository(gitCommonDir, commonOpts)
	wt := newWorktree(gitDir, rootDir, repo, commonOpts)
	return wt, nil
}

// Open opens the repository at the given directory.
// If dir is empty, the current working directory is used.
func Open(ctx context.Context, dir string, opts OpenOptions) (*Repository, error) {
	if opts.exec == nil {
		opts.exec = _realExec
	}
	log := opts.Log
	if log == nil {
		log = silog.Nop()
	}
	commonOpts := commonOptions{
		log:              log,
		exec:             opts.exec,
		indexLockTimeout: _defaultIndexLockTimeout,
	}
	if opts.IndexLockTimeout != nil {
		commonOpts.indexLockTimeout = max(*opts.IndexLockTimeout, 0)
	}

	gitDir, err := newGitCmd(ctx, commonOpts.log, commonOpts.exec,
		"rev-parse",
		"--path-format=absolute",
		"--git-common-dir",
	).WithDir(dir).OutputChomp()
	if err != nil {
		return nil, err
	}

	return newRepository(gitDir, commonOpts), nil
}

// CloneOptions configures the behavior of [Clone].
type CloneOptions struct {
	// Log specifies the logger to use for messages.
	Log *silog.Logger

	// IndexLockTimeout configures how long to spend retrying
	// git commands that fail because the index lock is held.
	// If nil, the package default is used.
	// If zero, retries are disabled.
	IndexLockTimeout *time.Duration

	exec execer
}

// Clone clones a Git repository from the given URL to the given directory.
func Clone(ctx context.Context, url, dir string, opts CloneOptions) (*Worktree, error) {
	if opts.exec == nil {
		opts.exec = _realExec
	}

	if opts.Log != nil {
		opts.Log.Debug("Cloning repository", "url", url, "destination", dir)
	}
	cloneCmd := newGitCmd(ctx, opts.Log, opts.exec, "clone", url, dir)
	if err := cloneCmd.Run(); err != nil {
		return nil, fmt.Errorf("git clone: %w", err)
	}

	return OpenWorktree(ctx, dir, OpenOptions(opts))
}

// Repository is a handle to a Git repository.
// It provides read-write access to the repository's contents.
type Repository struct {
	gitDir string

	log  *silog.Logger
	exec execer

	indexLockTimeout time.Duration
}

func newRepository(gitDir string, opts commonOptions) *Repository {
	return &Repository{
		gitDir:           gitDir,
		log:              opts.log,
		exec:             opts.exec,
		indexLockTimeout: opts.indexLockTimeout,
	}
}

// WithLogger returns a copy of the repository
// that will use the given logger.
func (r *Repository) WithLogger(log *silog.Logger) *Repository {
	newR := *r
	newR.log = log
	return &newR
}

// gitCmd returns a Git command wrapper
// configured to run in this repository.
func (r *Repository) gitCmd(ctx context.Context, subcmd string, args ...string) *gitCmd {
	return newGitCmd(ctx, r.log, r.exec, subcmd, args...).WithDir(r.gitDir)
}

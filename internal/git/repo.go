package git

import (
	"context"
	"log"
)

// OpenOptions configures the behavior of Open.
type OpenOptions struct {
	Log *log.Logger

	exec execer
}

// Open opens the repository at the given directory.
// If dir is empty, the current working directory is used.
func Open(ctx context.Context, dir string, opts OpenOptions) (*Repository, error) {
	if opts.exec == nil {
		opts.exec = _realExec
	}

	root, err := newGitCmd(ctx, opts.Log, "rev-parse", "--show-toplevel").
		Dir(dir).
		OutputString(opts.exec)
	if err != nil {
		return nil, err
	}

	return &Repository{
		root: root,
		log:  opts.Log,
		exec: opts.exec,
	}, nil
}

// Repository is a handle to a Git repository.
type Repository struct {
	root string
	log  *log.Logger
	exec execer
}

func (r *Repository) gitCmd(ctx context.Context, args ...string) *gitCmd {
	return newGitCmd(ctx, r.log, args...).Dir(r.root)
}

// HeadCommit reports the commit ID of HEAD.
func (r *Repository) HeadCommit(ctx context.Context) (string, error) {
	return r.gitCmd(ctx, "rev-parse", "HEAD").OutputString(r.exec)
}

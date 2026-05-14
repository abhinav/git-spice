package submodule

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

// Storage constants for the git-spice data store.
// Duplicated from main package to avoid an import cycle.
// Keep in sync with main package constants.
const (
	storageDataRef     = "refs/spice/data"
	storageAuthorName  = "git-spice"
	storageAuthorEmail = "git-spice@localhost"
)

// Context bundles the per-submodule plumbing
// (worktree, repository, store, service) needed to run
// git-spice handlers inside a submodule's working tree.
type Context struct {
	Path       string
	Worktree   *git.Worktree
	Repository *git.Repository
	Store      *state.Store
	Service    *spice.Service
	Log        *silog.Logger
}

// OpenContext opens the submodule at path under parentWT
// and constructs the git-spice plumbing rooted at that worktree.
//
// Returns [ErrSubmoduleNotInitialized] if the submodule has not been
// initialized with `gs repo init`. Callers should treat that as a
// soft signal (skip) rather than a hard error.
//
// forges may be nil for callers that only need worktree/store access.
// It is required for callers that will exercise forge-aware operations
// (e.g., submit) inside the submodule.
func OpenContext(
	ctx context.Context,
	parentWT *git.Worktree,
	path string,
	forges *forge.Registry,
	log *silog.Logger,
) (*Context, error) {
	subWT, err := parentWT.SubmoduleWorktree(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("open worktree: %w", err)
	}
	subRepo := subWT.Repository()

	db := storage.NewDB(storage.NewGitBackend(storage.GitConfig{
		Repo:        subRepo.WithLogger(log.Downgrade()),
		Ref:         storageDataRef,
		AuthorName:  storageAuthorName,
		AuthorEmail: storageAuthorEmail,
		Log:         log,
	}))

	store, err := state.OpenStore(ctx, db, log)
	if err != nil {
		if errors.Is(err, state.ErrUninitialized) {
			return nil, fmt.Errorf(
				"submodule %s: %w", path, ErrSubmoduleNotInitialized,
			)
		}
		return nil, fmt.Errorf(
			"submodule %s: open store: %w", path, err,
		)
	}

	svc := spice.NewService(subRepo, subWT, store, forges, log)

	return &Context{
		Path:       path,
		Worktree:   subWT,
		Repository: subRepo,
		Store:      store,
		Service:    svc,
		Log:        log,
	}, nil
}

// ForEachInitializedSubmodule iterates each submodule registered
// in parentWT's `.gitmodules`, skipping submodules whose path
// appears in exclude and submodules that are not gs-initialized
// (a soft skip; logged at info level).
//
// For each remaining submodule, a [Context] is constructed
// and fn is invoked. Iteration stops on the first error from fn.
func ForEachInitializedSubmodule(
	ctx context.Context,
	parentWT *git.Worktree,
	exclude []string,
	forges *forge.Registry,
	log *silog.Logger,
	fn func(*Context) error,
) error {
	subs, err := parentWT.Submodules(ctx)
	if err != nil {
		return fmt.Errorf("list submodules: %w", err)
	}

	for _, sub := range subs {
		if slices.Contains(exclude, sub.Path) {
			log.Debug("Skipping excluded submodule",
				"path", sub.Path)
			continue
		}

		subCtx, err := OpenContext(ctx, parentWT, sub.Path, forges, log)
		if err != nil {
			if errors.Is(err, ErrSubmoduleNotInitialized) {
				log.Info("Skipping submodule (not initialized with gs)",
					"path", sub.Path)
				continue
			}
			return err
		}

		if err := fn(subCtx); err != nil {
			return err
		}
	}

	return nil
}

package spice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
)

// LoadBranchItem is a single branch returned by LoadBranches.
type LoadBranchItem struct {
	// Name is the name of the branch.
	Name string

	// Head is the commit at the head of the branch.
	Head git.Hash

	// Base is the name of the branch that this branch is based on.
	Base string

	// BaseHash is the last known commit hash of the base branch.
	// This may not match the current commit hash of the base branch.
	BaseHash git.Hash

	// Change is the metadata associated with the branch.
	// This is nil if the branch has not been published.
	Change forge.ChangeMetadata

	// UpstreamBranch is the name under which this branch
	// was pushed to the upstream repository.
	UpstreamBranch string

	// MergedDownstack contains information about any branches,
	// which this one was based on, that have already been merged into trunk.
	MergedDownstack []json.RawMessage
}

// LoadBranches loads all tracked branches
// and all their information as a single operation.
//
// The returned branches are sorted by name.
func (s *Service) LoadBranches(ctx context.Context) ([]LoadBranchItem, error) {
	var (
		wg sync.WaitGroup

		mu sync.Mutex

		errs  []error
		items []LoadBranchItem

		// These will be used if we encounter any branches
		// that have been deleted out of band.
		deletedBranches = make(map[string]*DeletedBranchError)
	)
	namec := make(chan string)
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for name := range namec {
				resp, err := s.LookupBranch(ctx, name)
				if err != nil {
					if delErr := new(DeletedBranchError); errors.As(err, &delErr) {
						s.log.Infof("%v: removing...", delErr)
						mu.Lock()
						deletedBranches[name] = delErr
						mu.Unlock()
						continue
					}

					mu.Lock()
					errs = append(errs, fmt.Errorf("get branch %v: %w", name, err))
					mu.Unlock()
					continue
				}

				mu.Lock()
				items = append(items, LoadBranchItem{
					Name:            name,
					Head:            resp.Head,
					Base:            resp.Base,
					BaseHash:        resp.BaseHash,
					UpstreamBranch:  resp.UpstreamBranch,
					Change:          resp.Change,
					MergedDownstack: resp.MergedDownstack,
				})
				mu.Unlock()
			}
		})
	}

	for name, err := range s.store.ListBranches(ctx) {
		if err != nil {
			return nil, fmt.Errorf("list branches: %w", err)
		}
		namec <- name
	}
	close(namec)
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	slices.SortFunc(items, func(a, b LoadBranchItem) int {
		return strings.Compare(a.Name, b.Name)
	})

	if len(deletedBranches) == 0 {
		return items, nil
	}

	// Some of the branches we've loaded have been deleted out of band.
	// We'll delete these from the data store.
	tx := s.store.BeginBranchTx()

	// But first, we need to point the branches above deletes branches
	// to the bases of the deleted branches, or the bases of the bases,
	// and so on until we find a base that is not deleted.
	//
	// This will also update the LoadBranchItem instances
	// to reflect these changes so we're not re-reading the state.
	for i, item := range items {
		origBase := item.Base
		base, baseHash := item.Base, item.BaseHash

		delErr, deleted := deletedBranches[base]
		for deleted {
			base, baseHash = delErr.Base, delErr.BaseHash
			delErr, deleted = deletedBranches[base]
		}

		if base != origBase {
			if err := tx.Upsert(ctx, state.UpsertRequest{
				Name:     item.Name,
				Base:     base,
				BaseHash: baseHash,
			}); err != nil {
				s.log.Warn("Could not update base of branch upstack from deleted branch",
					"branch", item.Name,
					"newBase", item.Base,
					"error", err,
				)
				continue
			}

			item.Base = base
			item.BaseHash = baseHash
			items[i] = item
		}
	}

	// At this point, the deleted branches should not have any branches above them,
	// except those that we failed to update above.
	// Delete what we can, log the rest.
	for name := range deletedBranches {
		if err := tx.Delete(ctx, name); err != nil {
			s.log.Warn("Unable to delete branch", "branch", name, "error", err)
		}
	}

	if err := tx.Commit(ctx, "clean up deleted branches"); err != nil {
		s.log.Warn("Error cleaning up after deleted branches", "error", err)
	}

	return items, nil
}

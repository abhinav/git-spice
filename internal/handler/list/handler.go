// Package list defines handlers that list the repository state.
package list

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"runtime"
	"slices"
	"strings"
	"sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

// GitRepository lists operations from git.Repository
// that we need for the log handler.
type GitRepository interface {
	RemoteURL(context.Context, string) (string, error)
	CommitAheadBehind(context.Context, string, string) (int, int, error)
	ListCommitsDetails(context.Context, git.CommitRange) iter.Seq2[git.CommitDetail, error]
}

var _ GitRepository = (*git.Repository)(nil)

// Store provides access to git-spice's state store.
type Store interface {
	Remote() (string, error)
	Trunk() string
}

var _ Store = (*state.Store)(nil)

// Service provides access to git-spice's higher-level operations.
type Service interface {
	BranchGraph(context.Context, *spice.BranchGraphOptions) (*spice.BranchGraph, error)
	CheckRestacked(context.Context, string) (git.Hash, error)
}

var _ Service = (*spice.Service)(nil)

// Handler implements the business logic for git-spice's log commands.
type Handler struct {
	Log        *silog.Logger   // required
	Repository GitRepository   // required
	Store      Store           // required
	Service    Service         // required
	Forges     *forge.Registry // required

	// OpenRemoteRepository opens the remote repository
	// for the given remote URL.
	OpenRemoteRepository func(
		ctx context.Context,
		f forge.Forge,
		repo forge.RepositoryID,
	) (forge.Repository, error) // required
}

// Options holds command line options for the log command.
type Options struct {
	All bool `short:"a" long:"all" config:"log.all" help:"Show all tracked branches, not just the current stack."`
}

// Include specifies what additional information to include in the response.
type Include int

const (
	// IncludeMinimal includes only basic branch information.
	IncludeMinimal = 1 << iota

	// IncludeCommits includes a list of commits for each branch.
	IncludeCommits

	// IncludeChangeURL includes the URL for the associated change, if any.
	IncludeChangeURL

	// IncludePushStatus includes push status information for each branch
	// (e.g. ahead/behind counts).
	IncludePushStatus

	// IncludeChangeState includes the current forge change state for
	// branches that have an associated ChangeID.
	IncludeChangeState

	needsRemoteID = IncludeChangeURL | IncludeChangeState
)

// BranchesRequest holds the parameters for the log command.
type BranchesRequest struct {
	// Branch is the name of the branch to start from.
	// By default, only branches that are part of this branch's stack
	// are included.
	//
	// If Options.All is set, this is ignored and all tracked branches
	Branch string // required

	Options *Options
	Include Include
}

// BranchesResponse holds the result of the log handler.
type BranchesResponse struct {
	Branches []*BranchItem
	TrunkIdx int
}

// BranchItem is a single branch in the log output.
type BranchItem struct {
	// Name of the branch.
	Name string

	// Base branch onto which this branch is stacked.
	// Empty if this branch is trunk.
	Base string

	// Aboves lists branches that are stacked directly above this branch.
	Aboves []int

	Commits []git.CommitDetail // only if IncludeCommits is set

	// ChangeID is the ID of the associated change, if any.
	ChangeID    forge.ChangeID
	ChangeURL   string            // only if IncludeChangeURL is set
	ChangeState forge.ChangeState // populated if RemoteRepository is available
	PushStatus  *PushStatus       // only if IncludePushStatus is set

	// NeedsRestack indicates whether this branch needs to be restacked
	// on top of its base branch.
	NeedsRestack bool
}

// PushStatus contains push-related information
// if the branch has been pushed to a remote.
type PushStatus struct {
	// Ahead and Behind specify the number of commits
	// that the branch is ahead or behind its remote tracking branch.
	Ahead, Behind int

	// NeedsPush indicates whether the branch has commits
	// that need to be pushed to the remote.
	//
	// This will be false if Ahead and Behind are both zero.
	NeedsPush bool
}

// ListBranches logs the branches in the repository
// according to the request parameters.
func (h *Handler) ListBranches(ctx context.Context, req *BranchesRequest) (*BranchesResponse, error) {
	req.Options = cmp.Or(req.Options, &Options{})
	log := h.Log

	branchGraph, err := h.Service.BranchGraph(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("load branch graph: %w", err)
	}

	getRemote := sync.OnceValue(func() string {
		remote, err := h.Store.Remote()
		if err != nil {
			return ""
		}
		return remote
	})

	var (
		remoteForge  forge.Forge
		remoteRepoID forge.RepositoryID
	)
	if req.Include&needsRemoteID != 0 {
		err := func() error {
			remote := getRemote()

			remoteURL, err := h.Repository.RemoteURL(ctx, remote)
			if err != nil {
				return fmt.Errorf("get remote URL: %w", err)
			}

			var ok bool
			remoteForge, remoteRepoID, ok = forge.MatchRemoteURL(h.Forges, remoteURL)
			if !ok {
				return fmt.Errorf("no forge matches remote URL %q", remoteURL)
			}

			return nil
		}()
		if err != nil {
			log.Warn("Could not find information about the remote", "error", err)
		}
	}

	// changeURL queries the forge for the URL of a change request.
	changeURL := func(changeID forge.ChangeID) string {
		if remoteRepoID == nil {
			// No forge to query against. Just return the change ID.
			return changeID.String()
		}

		return remoteRepoID.ChangeURL(changeID)
	}

	var itemsMu sync.Mutex
	items := make([]*BranchItem, 0, branchGraph.Count()+1)   // +1 for trunk
	itemByName := make(map[string]*BranchItem, len(items)+1) // name -> item

	type branchLogEntry struct {
		Name string
	}

	entryc := make(chan branchLogEntry)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for entry := range entryc {
				if entry.Name == branchGraph.Trunk() {
					// Trunk is added at the end manually.
					continue
				}

				branch, ok := branchGraph.Lookup(entry.Name)
				if !ok {
					log.Warn("Branch disappeared from graph. Skipping.", "branch", entry.Name)
					continue
				}

				item := &BranchItem{
					Name: branch.Name,
				}

				// NB:
				// DO NOT 'continue' from this loop
				// as that will leave unfilled entries in infos,
				// which will panic down below when consuming
				// the result.

				// Check restack status /before/ looking up
				// the branch in git because VerifyRestacked
				// might update the branch's base hash
				// if the branch was manually restacked.
				//
				// TODO: This is a hack.
				// The isn't a good abstraction.
				baseHash, err := h.Service.CheckRestacked(ctx, branch.Name)
				if err != nil {
					var needsRestack *spice.BranchNeedsRestackError
					if errors.As(err, &needsRestack) {
						// if the branch needs to be restacked,
						// use the base hash stored in state
						// so that the log doesn't show duplicated commits.
						item.NeedsRestack = true
						baseHash = branch.BaseHash
					} else {
						baseHash = git.ZeroHash
					}
				}

				item.Base = branch.Base

				if branch.Change != nil {
					item.ChangeID = branch.Change.ChangeID()
					if req.Include&IncludeChangeURL != 0 {
						item.ChangeURL = changeURL(item.ChangeID)
					}
				}

				if req.Include&IncludePushStatus != 0 && branch.UpstreamBranch != "" {
					upstream := getRemote() + "/" + branch.UpstreamBranch
					ahead, behind, err := h.Repository.CommitAheadBehind(ctx, upstream, string(branch.Head))
					if err == nil {
						item.PushStatus = &PushStatus{
							Ahead:     ahead,
							Behind:    behind,
							NeedsPush: ahead > 0 || behind > 0,
						}
					}
				}

				if req.Include&IncludeCommits != 0 && baseHash != git.ZeroHash {
					commits, err := sliceutil.CollectErr(h.Repository.ListCommitsDetails(ctx,
						git.CommitRangeFrom(branch.Head).
							ExcludeFrom(baseHash).
							FirstParent()))
					if err != nil {
						log.Warn("Could not list commits for branch. Skipping.", "branch", branch.Name, "error", err)
					} else {
						item.Commits = commits
					}
				}

				itemsMu.Lock()
				items = append(items, item)
				itemByName[branch.Name] = item
				itemsMu.Unlock()
			}
		})
	}

	var branchesToLog iter.Seq[string]
	if req.Options.All {
		branchesToLog = branchGraph.Names()
	} else {
		// If req.Branch is not tracked,
		// we still want to list all branches.
		if _, ok := branchGraph.Lookup(req.Branch); !ok {
			branchesToLog = branchGraph.Names()
		} else {
			branchesToLog = branchGraph.Stack(req.Branch)
		}
	}
	for branch := range branchesToLog {
		entryc <- branchLogEntry{Name: branch}
	}
	close(entryc)
	wg.Wait()

	// Add trunk.
	trunkItem := &BranchItem{Name: h.Store.Trunk()}
	items = append(items, trunkItem)
	itemByName[trunkItem.Name] = trunkItem

	slices.SortFunc(items, func(a, b *BranchItem) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Connect the Above relationships.
	var trunkIdx int
	for idx, item := range items {
		if item.Name == branchGraph.Trunk() {
			trunkIdx = idx
			continue
		}

		baseItem, ok := itemByName[item.Base]
		if !ok {
			continue
		}

		baseItem.Aboves = append(baseItem.Aboves, idx)
	}

	// If requested and possible, batch-resolve ChangeState for items with ChangeID.
	if req.Include&IncludeChangeState != 0 && remoteForge != nil {
		// Try to load change states, but don't fail the whole operation
		// if something goes wrong.
		if err := h.loadChangeStates(ctx, remoteForge, remoteRepoID, items); err != nil {
			log.Warn("Could not load change states", "error", err)
		}
	}

	return &BranchesResponse{
		TrunkIdx: trunkIdx,
		Branches: items,
	}, nil
}

func (h *Handler) loadChangeStates(
	ctx context.Context,
	remoteForge forge.Forge,
	remoteRepoID forge.RepositoryID,
	branches []*BranchItem,
) error {
	// Collect IDs in the same order as items for stable mapping.
	branchesIdx := make([]int, 0, len(branches)) // index in items
	changeIDs := make([]forge.ChangeID, 0, len(branches))
	// For each changeIDs[i], branchesIdx[i] is the index in branches.
	for i, b := range branches {
		if b.ChangeID != nil {
			branchesIdx = append(branchesIdx, i)
			changeIDs = append(changeIDs, b.ChangeID)
		}
	}

	if len(changeIDs) == 0 {
		return nil
	}

	remoteRepo, err := h.OpenRemoteRepository(ctx, remoteForge, remoteRepoID)
	if err != nil {
		return fmt.Errorf("open remote repository: %w", err)
	}

	states, err := remoteRepo.ChangesStates(ctx, changeIDs)
	if err != nil {
		return fmt.Errorf("retrieve change states: %w", err)
	}

	for j, idx := range branchesIdx {
		branches[idx].ChangeState = states[j]
	}

	return nil
}

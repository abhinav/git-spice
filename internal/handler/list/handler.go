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
	"golang.org/x/sync/errgroup"
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
	Remote() (state.Remote, error)
	Trunk() string
	Integration(ctx context.Context) (*state.IntegrationInfo, error)

	// TrunkFor returns the trunk branch for the given worktree root:
	// the worktree's registered trunk if any, else the canonical trunk.
	TrunkFor(worktreePath string) string

	// Anchors returns all registered anchors (per-worktree trunks).
	Anchors() []state.Anchor
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
	Log        *silog.Logger // required
	Repository GitRepository // required
	Store      Store         // required
	Service    Service       // required

	// ResolveRepository resolves an upstream remote
	// to its forge repository identity.
	ResolveRepository func(
		context.Context,
		string,
	) (forge.Forge, forge.RepositoryID, error) // required

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
	All      bool `short:"a" long:"all" config:"log.all" help:"Show all tracked branches, not just the current stack."`
	Worktree bool `short:"w" long:"worktree" help:"Filter to branches in the current worktree. Implies --all."`
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

	// IncludeCommentCounts includes comment resolution counts for
	// branches that have an associated ChangeID.
	IncludeCommentCounts

	// IncludeChecks includes per-change rolled-up and per-run check
	// state for branches that have an associated ChangeID.
	IncludeChecks

	needsRemoteID = IncludeChangeURL | IncludeChangeState |
		IncludeCommentCounts | IncludeChecks
)

// BranchesRequest holds the parameters for the log command.
type BranchesRequest struct {
	// Branch is the name of the branch to start from.
	// By default, only branches that are part of this branch's stack
	// are included.
	//
	// If Options.All is set, this is ignored and all tracked branches
	Branch string // required

	// CurrentWorktree is the absolute path
	// to the current worktree root.
	// Required when Options.Worktree is set.
	CurrentWorktree string

	Options *Options
	Include Include
}

// BranchesResponse holds the result of the log handler.
type BranchesResponse struct {
	Branches []*BranchItem
	TrunkIdx int

	// IntegrationIdx is the index of the synthetic integration branch
	// item in Branches, or -1 if no integration branch is configured.
	IntegrationIdx int
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
	ChangeID      forge.ChangeID
	ChangeURL     string               // only if IncludeChangeURL is set
	ChangeState   forge.ChangeState    // populated if RemoteRepository is available
	CommentCounts *forge.CommentCounts // only if IncludeCommentCounts is set
	Checks        *forge.ChangeChecks  // only if IncludeChecks is set
	PushStatus    *PushStatus          // only if IncludePushStatus is set

	// Worktree is the absolute path to the worktree where this branch is checked out.
	// Empty if the branch is not checked out.
	Worktree string

	// NeedsRestack indicates whether this branch needs to be restacked
	// on top of its base branch.
	NeedsRestack bool

	// Submodules maps submodule paths
	// to associated branch names.
	Submodules map[string]string

	// IntegrationTip indicates that this branch is a configured tip of
	// the integration branch.
	IntegrationTip bool

	// Integration is non-nil for the synthetic integration branch row.
	// Regular branch rows always have nil Integration.
	Integration *IntegrationDisplay

	// Anchor reports whether this item is an anchor branch:
	// a per-worktree trunk that roots another worktree's stack.
	// Anchor items are injected as display nodes; they are not
	// tracked stack branches.
	Anchor bool

	// AnchorBase is the branch an internal anchor is pinned at.
	// Empty for a root anchor (which tracks the remote trunk).
	// Only meaningful when Anchor is true.
	AnchorBase string
}

// IntegrationDisplay carries presentation-relevant information about
// the configured integration branch.
type IntegrationDisplay struct {
	// Tips lists the configured tip branch names in declaration order.
	Tips []string
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

	branchGraph, err := h.Service.BranchGraph(ctx, &spice.BranchGraphOptions{
		IncludeWorktrees: true,
	})
	if err != nil {
		return nil, fmt.Errorf("load branch graph: %w", err)
	}

	getRemote := sync.OnceValue(func() state.Remote {
		remote, err := h.Store.Remote()
		if errors.Is(err, state.ErrNotExist) {
			return state.Remote{}
		}
		if err != nil {
			log.Warn("Could not load remote configuration", "error", err)
			return state.Remote{}
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
			if remote == (state.Remote{}) {
				return nil
			}

			var err error
			remoteForge, remoteRepoID, err = h.ResolveRepository(ctx, remote.Upstream)
			return err
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

	// displayTrunk is the trunk shown as the root of the listing. In a
	// linked worktree with its own trunk, that is the worktree's local
	// trunk so the stacks based on it render under it; elsewhere it is
	// the canonical trunk.
	displayTrunk := h.Store.TrunkFor(req.CurrentWorktree)

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
				if entry.Name == displayTrunk {
					// Trunk is added at the end manually.
					continue
				}

				branch, ok := branchGraph.Lookup(entry.Name)
				if !ok {
					log.Warn("Branch disappeared from graph. Skipping.", "branch", entry.Name)
					continue
				}

				item := &BranchItem{
					Name:     branch.Name,
					Worktree: branchGraph.Worktree(branch.Name),
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
				item.Submodules = branch.Submodules

				if branch.Change != nil {
					item.ChangeID = branch.Change.ChangeID()
					if req.Include&IncludeChangeURL != 0 {
						item.ChangeURL = changeURL(item.ChangeID)
					}
				}

				if req.Include&IncludePushStatus != 0 && branch.UpstreamBranch != "" {
					remote := getRemote()
					if remote != (state.Remote{}) {
						upstream := remote.Push + "/" + branch.UpstreamBranch
						ahead, behind, err := h.Repository.CommitAheadBehind(ctx, upstream, string(branch.Head))
						if err == nil {
							item.PushStatus = &PushStatus{
								Ahead:     ahead,
								Behind:    behind,
								NeedsPush: ahead > 0 || behind > 0,
							}
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
	switch {
	case req.Options.Worktree:
		// Filter to branches in the current worktree.
		// Include the full stack if any branch in it
		// is checked out in the current worktree.
		branchesToLog = func(yield func(string) bool) {
			for stack := range branchGraph.StacksInWorktree(
				req.CurrentWorktree,
			) {
				for _, branch := range stack {
					if !yield(branch) {
						return
					}
				}
			}
		}
	case req.Options.All:
		branchesToLog = branchGraph.Names()
	default:
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
	trunkItem := &BranchItem{Name: displayTrunk}
	items = append(items, trunkItem)
	itemByName[trunkItem.Name] = trunkItem

	// Load the integration branch configuration so the row can be
	// surfaced alongside regular branches, and tip branches can be
	// marked. A missing integration is not an error: most repos don't
	// have one.
	integration, err := h.Store.Integration(ctx)
	if errors.Is(err, state.ErrNotExist) {
		integration = nil
	} else if err != nil {
		log.Warn("Could not load integration branch configuration", "error", err)
		integration = nil
	}

	// Mark tip rows and synthesize an item for the integration branch
	// itself. The integration branch is repo-scoped (singleton) and
	// not tracked, so it has no base and no aboves.
	if integration != nil {
		tipNames := make([]string, 0, len(integration.Tips))
		for _, tip := range integration.Tips {
			tipNames = append(tipNames, tip.Name)
			if item, ok := itemByName[tip.Name]; ok {
				item.IntegrationTip = true
			}
		}
		intItem := &BranchItem{
			Name: integration.Name,
			Integration: &IntegrationDisplay{
				Tips: tipNames,
			},
		}
		items = append(items, intItem)
		itemByName[intItem.Name] = intItem
	}

	// Inject anchor nodes for any displayed branch rooted at an anchor.
	// Anchors are per-worktree trunks: graph roots that are not tracked
	// stack branches, so they are absent from the listing. Show each one
	// as an intermediate node under its base (the canonical trunk for a
	// root anchor, or the pinned branch for an internal anchor) so the
	// owning worktree's stack renders beneath it.
	anchorByBranch := make(map[string]state.Anchor)
	for _, a := range h.Store.Anchors() {
		anchorByBranch[a.Branch] = a
	}
	for _, item := range items {
		anchor, ok := anchorByBranch[item.Base]
		if !ok {
			continue // base is a normal branch or the trunk
		}
		if _, shown := itemByName[anchor.Branch]; shown {
			continue // already injected
		}

		base := cmp.Or(anchor.Base, displayTrunk)
		anchorItem := &BranchItem{
			Name:       anchor.Branch,
			Base:       base,
			Worktree:   anchor.Worktree,
			Anchor:     true,
			AnchorBase: anchor.Base,
		}
		items = append(items, anchorItem)
		itemByName[anchor.Branch] = anchorItem
	}

	slices.SortFunc(items, func(a, b *BranchItem) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Connect the Above relationships. Skip the integration row: it is
	// untracked and never appears as another branch's base.
	var (
		trunkIdx       int
		integrationIdx = -1
	)
	for idx, item := range items {
		switch {
		case item.Name == displayTrunk:
			trunkIdx = idx
			continue
		case item.Integration != nil:
			integrationIdx = idx
			continue
		}

		baseItem, ok := itemByName[item.Base]
		if !ok {
			continue
		}

		baseItem.Aboves = append(baseItem.Aboves, idx)
	}

	// If requested and possible, batch-resolve remote change metadata.
	if req.Include&(IncludeChangeState|IncludeCommentCounts|IncludeChecks) != 0 && remoteForge != nil {
		// Try to load remote metadata, but don't fail the whole operation
		// if something goes wrong.
		if err := h.loadRemoteChangeData(ctx, remoteForge, remoteRepoID, req.Include, items); err != nil {
			log.Warn("Could not load remote change data", "error", err)
		}
	}

	return &BranchesResponse{
		TrunkIdx:       trunkIdx,
		IntegrationIdx: integrationIdx,
		Branches:       items,
	}, nil
}

func (h *Handler) loadRemoteChangeData(
	ctx context.Context,
	remoteForge forge.Forge,
	remoteRepoID forge.RepositoryID,
	include Include,
	branches []*BranchItem,
) error {
	// Collect IDs in the same order as items for stable mapping.
	branchesIdx := make([]int, 0, len(branches))
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

	var updates []func()
	wg, ctx := errgroup.WithContext(ctx)

	if include&IncludeChangeState != 0 {
		var statuses []forge.ChangeStatus
		updates = append(updates, func() {
			for j, idx := range branchesIdx {
				branches[idx].ChangeState = statuses[j].State
			}
		})

		wg.Go(func() error {
			var err error
			statuses, err = remoteRepo.ChangeStatuses(ctx, changeIDs)
			if err != nil {
				return fmt.Errorf("retrieve change states: %w", err)
			}
			return nil
		})
	}

	if include&IncludeCommentCounts != 0 {
		var counts []*forge.CommentCounts
		updates = append(updates, func() {
			for j, idx := range branchesIdx {
				branches[idx].CommentCounts = counts[j]
			}
		})

		wg.Go(func() error {
			var err error
			counts, err = remoteRepo.CommentCountsByChange(ctx, changeIDs)
			if err != nil {
				return fmt.Errorf("retrieve comment counts: %w", err)
			}
			return nil
		})
	}

	if include&IncludeChecks != 0 {
		var checks []*forge.ChangeChecks
		updates = append(updates, func() {
			for j, idx := range branchesIdx {
				branches[idx].Checks = checks[j]
			}
		})

		wg.Go(func() error {
			var err error
			checks, err = remoteRepo.ChecksByChange(ctx, changeIDs)
			if err != nil {
				return fmt.Errorf("retrieve checks: %w", err)
			}
			return nil
		})
	}

	if err := wg.Wait(); err != nil {
		return err
	}

	for _, update := range updates {
		update()
	}

	return nil
}

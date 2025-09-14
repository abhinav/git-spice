// Package split implements logic for branch split commands.
package split

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
)

// GitRepository provides treeless read/write access to the Git state.
type GitRepository interface {
	BranchExists(ctx context.Context, branch string) bool
	PeelToCommit(ctx context.Context, ref string) (git.Hash, error)
	SetBranchUpstream(ctx context.Context, branch, upstream string) error
	BranchUpstream(ctx context.Context, branch string) (string, error)
	ListCommitsDetails(ctx context.Context, commits git.CommitRange) iter.Seq2[git.CommitDetail, error]
	SetRef(ctx context.Context, req git.SetRefRequest) error
}

var _ GitRepository = (*git.Repository)(nil)

// Store is the git-spice data store.
type Store interface {
	Trunk() string
	Remote() (string, error)
	BeginBranchTx() *state.BranchTx
}

var _ Store = (*state.Store)(nil)

// Service is a subset of spice.Service.
type Service interface {
	LookupBranch(ctx context.Context, name string) (*spice.LookupBranchResponse, error)
}

var _ Service = (*spice.Service)(nil)

// Handler handles gs's branch split commands.
type Handler struct {
	Log            *silog.Logger                            // required
	View           ui.View                                  // required
	Repository     GitRepository                            // required
	Store          Store                                    // required
	Service        Service                                  // required
	FindForge      func(forgeID string) (forge.Forge, bool) // required
	HighlightStyle lipgloss.Style                           // required
}

// Options defines options for the SplitBranch method.
// These are exposed as flags in the CLI
type Options struct {
	At []Point `placeholder:"COMMIT:NAME" help:"Commits to split the branch at."`
}

// Point represents a commit:name pair for splitting.
type Point struct {
	// Commit is the git commit hash to split at.
	Commit string

	// Name is the name of the new branch to create at the commit.
	Name string
}

// Decode parses a COMMIT:NAME specification into a BranchSplit.
func (b *Point) Decode(ctx *kong.DecodeContext) error {
	var spec string
	if err := ctx.Scan.PopValueInto("at", &spec); err != nil {
		return err
	}

	idx := strings.LastIndex(spec, ":")
	switch {
	case idx == -1:
		return fmt.Errorf("expected COMMIT:NAME, got %q", spec)
	case len(spec[:idx]) == 0:
		return fmt.Errorf("part before : cannot be empty: %q", spec)
	case len(spec[idx+1:]) == 0:
		return fmt.Errorf("part after : cannot be empty: %q", spec)
	}

	b.Commit = spec[:idx]
	b.Name = spec[idx+1:]
	return nil
}

// BranchRequest is a request to split a branch.
//
// The list of split points in the branch may be specified as options,
// or via SelectCommits.
// If a split point requests the original branch name,
// then there MUST be another split point for the HEAD commit
// (to take over the original branch name).
type BranchRequest struct {
	Branch  string   // required
	Options *Options // optional, defaults to nil

	// SelectCommits is a function that allows the user to select commits
	// for splitting the branch if the --at flag is not provided.
	SelectCommits func(context.Context, []git.CommitDetail) ([]Point, error)
}

// BranchResult is the result of splitting a branch.
type BranchResult struct {
	// Top is the name of the branch assigned to the topmost commit
	// after the split.
	//
	// This is normally the original branch name,
	// but if the original branch was reassigned to a lower commit,
	// this will be the name of the branch
	// assigned to the original HEAD commit.
	Top string
}

// SplitBranch splits a branch into two or more branches along commit boundaries.
func (h *Handler) SplitBranch(ctx context.Context, req *BranchRequest) (*BranchResult, error) {
	branch, opts := req.Branch, req.Options
	opts = cmp.Or(opts, &Options{})

	if branch == h.Store.Trunk() {
		return nil, errors.New("cannot split trunk")
	}

	branchInfo, err := h.Service.LookupBranch(ctx, branch)
	if err != nil {
		return nil, fmt.Errorf("lookup branch %q: %w", branch, err)
	}

	branchCommits, err := sliceutil.CollectErr(h.Repository.ListCommitsDetails(ctx,
		git.CommitRangeFrom(branchInfo.Head).
			ExcludeFrom(branchInfo.BaseHash).
			Reverse()))
	if err != nil {
		return nil, fmt.Errorf("list commits: %w", err)
	}

	branchCommitHashes := make(map[git.Hash]struct{}, len(branchCommits))
	for _, commit := range branchCommits {
		branchCommitHashes[commit.Hash] = struct{}{}
	}

	if len(opts.At) == 0 {
		newSplits, err := req.SelectCommits(ctx, branchCommits)
		if err != nil {
			return nil, err
		}
		opts.At = newSplits
	}

	commitHashes := make([]git.Hash, len(opts.At))
	newTakenNames := make(map[string]int, len(opts.At)) // name => index in opts.At
	var originalNameReused, headCommitIncluded bool
	var headBranchName string // name of branch assigned to HEAD commit
	for i, split := range opts.At {
		// TODO:
		// A bit annoying that we have to repeat validation
		// from the widget here. Refactor?
		if otherIdx, ok := newTakenNames[split.Name]; ok {
			return nil, fmt.Errorf("--at[%d]: branch name already taken by --at[%d]: %v", i, otherIdx, split.Name)
		}

		if split.Name != branch && h.Repository.BranchExists(ctx, split.Name) {
			return nil, fmt.Errorf("--at[%d]: branch already exists: %v", i, split.Name)
		}

		commitHash, err := h.Repository.PeelToCommit(ctx, split.Commit)
		if err != nil {
			return nil, fmt.Errorf("--at[%d]: resolve commit %q: %w", i, split.Commit, err)
		}

		if _, ok := branchCommitHashes[commitHash]; !ok {
			return nil, fmt.Errorf("--at[%d]: %v (%v) is not in range %v..%v", i,
				split.Commit, commitHash, branchInfo.Base, branch)
		}
		commitHashes[i] = commitHash

		// Record whether the original branch is being moved
		// and whether we have a new branch for HEAD for validation.
		if split.Name == branch {
			originalNameReused = true
		} else if commitHash == branchInfo.Head {
			headCommitIncluded = true
			headBranchName = split.Name
		}

		newTakenNames[split.Name] = i
	}

	if originalNameReused && !headCommitIncluded {
		oldHead := branchInfo.Head
		newHead := commitHashes[newTakenNames[branch]]

		h.Log.Errorf("%v: branch HEAD is being moved to %v, "+
			"but a name for the original HEAD (%v) was not provided",
			branch, newHead.Short(), oldHead.Short())
		return nil, fmt.Errorf("a new name for HEAD (%v) is required", oldHead.Short())
	}

	branchTx := h.Store.BeginBranchTx()
	for idx, split := range opts.At {
		base, baseHash := branchInfo.Base, branchInfo.BaseHash
		if idx > 0 {
			base, baseHash = opts.At[idx-1].Name, commitHashes[idx-1]
		}

		if err := branchTx.Upsert(ctx, state.UpsertRequest{
			Name:     split.Name,
			Base:     base,
			BaseHash: baseHash,
		}); err != nil {
			return nil, fmt.Errorf("add branch %v with base %v: %w", split.Name, base, err)
		}
		h.Log.Debug("Updating tracked branch state",
			"branch", split.Name,
			"base", base+"@"+baseHash.String())
	}

	// If the original branch is being moved, its state update
	// is covered above. Otherwise, we have to update it manually here.
	if !originalNameReused {
		finalBase, finalBaseHash := branchInfo.Base, branchInfo.BaseHash
		if len(opts.At) > 0 {
			finalBase, finalBaseHash = opts.At[len(opts.At)-1].Name, commitHashes[len(opts.At)-1]
		}

		if err := branchTx.Upsert(ctx, state.UpsertRequest{
			Name:     branch,
			Base:     finalBase,
			BaseHash: finalBaseHash,
		}); err != nil {
			return nil, fmt.Errorf("update branch %v with base %v: %w", branch, finalBase, err)
		}

		h.Log.Debug("Updating tracked branch state",
			"branch", branch,
			"base", finalBase+"@"+finalBaseHash.String())
	}

	if branchInfo.Change != nil && !ui.Interactive(h.View) {
		h.Log.Info("Branch has an associated CR. Leaving it assigned to the original branch.",
			"cr", branchInfo.Change.ChangeID())
	} else if branchInfo.Change != nil {
		branchNames := make([]string, 0, len(opts.At)+1)
		for _, split := range opts.At {
			branchNames = append(branchNames, split.Name)
		}
		if !originalNameReused {
			// If original branch is being moved,
			// it's already in the list.
			branchNames = append(branchNames, branch)
		}

		var changeBranch string
		prompt := ui.NewSelect[string]().
			WithTitle(fmt.Sprintf("Assign CR %v to branch", branchInfo.Change.ChangeID())).
			WithDescription("Branch being split has an open CR assigned to it.\n" +
				"Select which branch should take over the CR.").
			WithValue(&changeBranch).
			With(ui.ComparableOptions(branch, branchNames...))
		if err := ui.Run(h.View, prompt); err != nil {
			return nil, fmt.Errorf("prompt: %w", err)
		}

		if changeBranch != branch {
			transfer, err := h.prepareChangeMetadataTransfer(
				ctx,
				branch,
				changeBranch,
				branchInfo.Change,
				branchInfo.UpstreamBranch,
				branchTx,
			)
			if err != nil {
				return nil, fmt.Errorf("transfer CR %v to %v: %w", branchInfo.Change.ChangeID(), changeBranch, err)
			}

			defer func() {
				if err == nil {
					transfer()
				}
			}()
		}
	}

	for idx, split := range opts.At {
		oldHash := git.ZeroHash
		if split.Name == branch {
			// If this is moving the original branch,
			// ensure we don't clobber any changes
			// that happened since we looked it up.
			oldHash = branchInfo.Head
		}

		var reason string
		if split.Name == branch {
			reason = "gs: move branch head"
		} else {
			reason = "gs: create new branch"
		}

		if err := h.Repository.SetRef(ctx, git.SetRefRequest{
			Ref:     "refs/heads/" + split.Name,
			Hash:    commitHashes[idx],
			OldHash: oldHash,
			Reason:  reason,
		}); err != nil {
			return nil, fmt.Errorf("update branch %q: %w", split.Name, err)
		}
	}

	if err := branchTx.Commit(ctx, fmt.Sprintf("%v: split %d new branches", branch, len(opts.At))); err != nil {
		return nil, fmt.Errorf("update store: %w", err)
	}

	return &BranchResult{
		Top: cmp.Or(headBranchName, branch),
	}, nil
}

func (h *Handler) prepareChangeMetadataTransfer(
	ctx context.Context,
	fromBranch, toBranch string,
	meta forge.ChangeMetadata,
	upstreamBranch string,
	tx *state.BranchTx,
) (transfer func(), _ error) {
	forgeID := meta.ForgeID()
	f, ok := h.FindForge(forgeID)
	if !ok {
		return nil, fmt.Errorf("unknown forge: %v", forgeID)
	}

	remote, err := h.Store.Remote()
	if err != nil {
		return nil, fmt.Errorf("get remote: %w", err)
	}

	metaJSON, err := f.MarshalChangeMetadata(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal change metadata: %w", err)
	}

	toUpstreamBranch := cmp.Or(upstreamBranch, fromBranch)

	var empty string
	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:           fromBranch,
		ChangeMetadata: state.Null,
		UpstreamBranch: &empty,
	}); err != nil {
		return nil, fmt.Errorf("clear change metadata from %v: %w", fromBranch, err)
	}

	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:           toBranch,
		ChangeMetadata: metaJSON,
		ChangeForge:    forgeID,
		UpstreamBranch: &toUpstreamBranch,
	}); err != nil {
		return nil, fmt.Errorf("set change metadata on %v: %w", toBranch, err)
	}
	h.Log.Debug("Transferring submitted CR metadata between tracked branches",
		"from", fromBranch, "to", toBranch)

	return func() {
		if err := h.Repository.SetBranchUpstream(ctx, toBranch, remote+"/"+toUpstreamBranch); err != nil {
			h.Log.Warnf("%v: Failed to set upstream branch %v: %v", toBranch, toUpstreamBranch, err)
		}

		if _, err := h.Repository.BranchUpstream(ctx, fromBranch); err == nil {
			if err := h.Repository.SetBranchUpstream(ctx, fromBranch, ""); err != nil {
				h.Log.Warnf("%v: Failed to unset upstream branch %v: %v", fromBranch, upstreamBranch, err)
			}
		}

		h.Log.Infof("%v: Upstream branch '%v' transferred to '%v'", fromBranch, toUpstreamBranch, toBranch)
		if toUpstreamBranch == fromBranch {
			pushCmd := fmt.Sprintf("git push -u %v %v:<new name>", remote, fromBranch)

			h.Log.Warnf("%v: If you push this branch with 'git push' instead of 'gs branch submit',", fromBranch)
			h.Log.Warnf("%v: remember to use a different upstream branch name with the command:\n\t%s", fromBranch, h.HighlightStyle.Render(pushCmd))
		}
	}, nil
}

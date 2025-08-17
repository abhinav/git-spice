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
	CreateBranch(ctx context.Context, req git.CreateBranchRequest) error
	BranchExists(ctx context.Context, branch string) bool
	PeelToCommit(ctx context.Context, ref string) (git.Hash, error)
	SetBranchUpstream(ctx context.Context, branch, upstream string) error
	BranchUpstream(ctx context.Context, branch string) (string, error)
	ListCommitsDetails(ctx context.Context, commits git.CommitRange) iter.Seq2[git.CommitDetail, error]
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
type BranchRequest struct {
	Branch  string   // required
	Options *Options // optional, defaults to nil

	// SelectCommits is a function that allows the user to select commits
	// for splitting the branch if the --at flag is not provided.
	SelectCommits func(context.Context, []git.CommitDetail) ([]Point, error)
}

// SplitBranch splits a branch into two or more branches along commit boundaries.
func (h *Handler) SplitBranch(ctx context.Context, req *BranchRequest) error {
	branch, opts := req.Branch, req.Options
	opts = cmp.Or(opts, &Options{})

	if branch == h.Store.Trunk() {
		return errors.New("cannot split trunk")
	}

	branchInfo, err := h.Service.LookupBranch(ctx, branch)
	if err != nil {
		return fmt.Errorf("lookup branch %q: %w", branch, err)
	}

	branchCommits, err := sliceutil.CollectErr(h.Repository.ListCommitsDetails(ctx,
		git.CommitRangeFrom(branchInfo.Head).
			ExcludeFrom(branchInfo.BaseHash).
			Reverse()))
	if err != nil {
		return fmt.Errorf("list commits: %w", err)
	}

	branchCommitHashes := make(map[git.Hash]struct{}, len(branchCommits))
	for _, commit := range branchCommits {
		branchCommitHashes[commit.Hash] = struct{}{}
	}

	if len(opts.At) == 0 {
		newSplits, err := req.SelectCommits(ctx, branchCommits)
		if err != nil {
			return err
		}
		opts.At = newSplits

	}

	commitHashes := make([]git.Hash, len(opts.At))
	newTakenNames := make(map[string]int, len(opts.At))
	for i, split := range opts.At {
		if h.Repository.BranchExists(ctx, split.Name) {
			return fmt.Errorf("--at[%d]: branch already exists: %v", i, split.Name)
		}

		if otherIdx, ok := newTakenNames[split.Name]; ok {
			return fmt.Errorf("--at[%d]: branch name already taken by --at[%d]: %v", i, otherIdx, split.Name)
		}
		newTakenNames[split.Name] = i

		commitHash, err := h.Repository.PeelToCommit(ctx, split.Commit)
		if err != nil {
			return fmt.Errorf("--at[%d]: resolve commit %q: %w", i, split.Commit, err)
		}

		if _, ok := branchCommitHashes[commitHash]; !ok {
			return fmt.Errorf("--at[%d]: %v (%v) is not in range %v..%v", i,
				split.Commit, commitHash, branchInfo.Base, branch)
		}
		commitHashes[i] = commitHash
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
			return fmt.Errorf("add branch %v with base %v: %w", split.Name, base, err)
		}
		h.Log.Debug("Updating tracked branch state",
			"branch", split.Name,
			"base", base+"@"+baseHash.String())
	}

	finalBase, finalBaseHash := branchInfo.Base, branchInfo.BaseHash
	if len(opts.At) > 0 {
		finalBase, finalBaseHash = opts.At[len(opts.At)-1].Name, commitHashes[len(opts.At)-1]
	}
	if err := branchTx.Upsert(ctx, state.UpsertRequest{
		Name:     branch,
		Base:     finalBase,
		BaseHash: finalBaseHash,
	}); err != nil {
		return fmt.Errorf("update branch %v with base %v: %w", branch, finalBase, err)
	}
	h.Log.Debug("Updating tracked branch state",
		"branch", branch,
		"base", finalBase+"@"+finalBaseHash.String())

	if branchInfo.Change != nil && !ui.Interactive(h.View) {
		h.Log.Info("Branch has an associated CR. Leaving it assigned to the original branch.",
			"cr", branchInfo.Change.ChangeID())
	} else if branchInfo.Change != nil {
		branchNames := make([]string, len(opts.At)+1)
		for i, split := range opts.At {
			branchNames[i] = split.Name
		}
		branchNames[len(branchNames)-1] = branch

		var changeBranch string
		prompt := ui.NewSelect[string]().
			WithTitle(fmt.Sprintf("Assign CR %v to branch", branchInfo.Change.ChangeID())).
			WithDescription("Branch being split has an open CR assigned to it.\n" +
				"Select which branch should take over the CR.").
			WithValue(&changeBranch).
			With(ui.ComparableOptions(branch, branchNames...))
		if err := ui.Run(h.View, prompt); err != nil {
			return fmt.Errorf("prompt: %w", err)
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
				return fmt.Errorf("transfer CR %v to %v: %w", branchInfo.Change.ChangeID(), changeBranch, err)
			}

			defer func() {
				if err == nil {
					transfer()
				}
			}()
		}
	}

	for idx, split := range opts.At {
		if err := h.Repository.CreateBranch(ctx, git.CreateBranchRequest{
			Name: split.Name,
			Head: commitHashes[idx].String(),
		}); err != nil {
			return fmt.Errorf("create branch %q: %w", split.Name, err)
		}
	}

	if err := branchTx.Commit(ctx, fmt.Sprintf("%v: split %d new branches", branch, len(opts.At))); err != nil {
		return fmt.Errorf("update store: %w", err)
	}

	return nil
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

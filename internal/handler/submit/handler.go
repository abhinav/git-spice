// Package submit implements change submission handling.
// This is used by the various 'submit' commands in the CLI.
package submit

import (
	"cmp"
	"context"
	"encoding"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.abhg.dev/gs/internal/browser"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/iterutil"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
)

// GitRepository is a subset of the git.Repository interface
// that is used by the submit handler.
type GitRepository interface {
	PeelToCommit(ctx context.Context, ref string) (git.Hash, error)
	PeelToTree(ctx context.Context, ref string) (git.Hash, error)
	BranchUpstream(ctx context.Context, branch string) (string, error)
	SetBranchUpstream(ctx context.Context, branch string, upstream string) error
	Var(ctx context.Context, name string) (string, error)
	CommitMessageRange(ctx context.Context, start string, stop string) ([]git.CommitMessage, error)
	RemoteFetchRefspecs(ctx context.Context, remote string) ([]git.Refspec, error)
}

var _ GitRepository = (*git.Repository)(nil)

// GitWorktree is a subset of the git.Worktree interface
// that is used by the submit handler.
type GitWorktree interface {
	Push(ctx context.Context, opts git.PushOptions) error
}

var _ GitWorktree = (*git.Worktree)(nil)

// Store provides read/write access to the state store.
type Store interface {
	BeginBranchTx() *state.BranchTx
	Trunk() string

	LoadPreparedBranch(ctx context.Context, name string) (*state.PreparedBranch, error)
	SavePreparedBranch(ctx context.Context, b *state.PreparedBranch) error
	ClearPreparedBranch(ctx context.Context, name string) error
}

var _ Store = (*state.Store)(nil)

// Service provides access to the Spice service.
type Service interface {
	LoadBranches(context.Context) ([]spice.LoadBranchItem, error)
	VerifyRestacked(ctx context.Context, name string) error
	LookupBranch(ctx context.Context, name string) (*spice.LookupBranchResponse, error)
	UnusedBranchName(ctx context.Context, remote string, branch string) (string, error)
	ListChangeTemplates(context.Context, string, forge.Repository) ([]*forge.ChangeTemplate, error)
}

var _ Service = (*spice.Service)(nil)

//go:generate mockgen -typed -destination mocks_test.go -package submit . Service

// Handler implements support for submission of change requests.
type Handler struct {
	Log        *silog.Logger    // required
	View       ui.View          // required
	Repository GitRepository    // required
	Worktree   GitWorktree      // required
	Store      Store            // required
	Service    Service          // required
	Browser    browser.Launcher // required

	// TODO: these should not be a func reference
	// this whole memoize thing is a bit of a hack
	FindRemote           func(ctx context.Context) (string, error)                          // required
	OpenRemoteRepository func(ctx context.Context, remote string) (forge.Repository, error) // required
	remote               memoizedValue[string]
	remoteRepository     memoizedValue[forge.Repository]
}

// Remote returns the remote name for the current repository,
// memoizing the result.
func (h *Handler) Remote(ctx context.Context) (string, error) {
	return h.remote.Get(func() (string, error) {
		return h.FindRemote(ctx)
	})
}

// RemoteRepository returns the remote repository for the current repository,
// memoizing the result.
func (h *Handler) RemoteRepository(ctx context.Context) (forge.Repository, error) {
	return h.remoteRepository.Get(func() (forge.Repository, error) {
		remote, err := h.Remote(ctx)
		if err != nil {
			return nil, fmt.Errorf("get remote: %w", err)
		}

		return h.OpenRemoteRepository(ctx, remote)
	})
}

type memoizedValue[T any] struct {
	once  sync.Once
	value T
	err   error
}

func (v *memoizedValue[T]) Get(get func() (T, error)) (T, error) {
	v.once.Do(func() {
		v.value, v.err = get()
	})
	return v.value, v.err
}

// Options defines options for the submit operations.
//
// Options translate into user-facing command line flags or configuration options,
// so care must be taken when adding things here.
type Options struct {
	DryRun bool `short:"n" help:"Don't actually submit the stack"`
	Fill   bool `short:"c" help:"Fill in the change title and body from the commit messages"`
	// TODO: Default to Fill if --no-prompt?
	Draft   *bool   `negatable:"" help:"Whether to mark change requests as drafts"`
	Publish bool    `name:"publish" negatable:"" default:"true" config:"submit.publish" help:"Whether to create CRs for pushed branches. Defaults to true."`
	Web     OpenWeb `short:"w" config:"submit.web" help:"Open submitted changes in a web browser. Accepts an optional argument: 'true', 'false', 'created'."`

	NavComment          NavCommentWhen      `name:"nav-comment" config:"submit.navigationComment" enum:"true,false,multiple" default:"true" help:"Whether to add a navigation comment to the change request. Must be one of: true, false, multiple."`
	NavCommentSync      NavCommentSync      `name:"nav-comment-sync" config:"submit.navigationCommentSync" enum:"branch,downstack" default:"branch" hidden:"" help:"Which navigation comment to sync. Must be one of: branch, downstack."`
	NavCommentDownstack NavCommentDownstack `name:"nav-comment-downstack" config:"submit.navigationComment.downstack" enum:"all,open" default:"all" hidden:"" help:"Which downstack CRs to include in navigation comments. Must be one of: all, open."`
	NavCommentMarker    string              `name:"nav-comment-marker" config:"submit.navigationCommentStyle.marker" hidden:"" help:"Marker to use for the current change in navigation comments. Defaults to '◀'."`

	Force      bool  `help:"Force push, bypassing safety checks"`
	NoVerify   bool  `help:"Bypass pre-push hooks when pushing to the remote." released:"v0.15.0"`
	UpdateOnly *bool `short:"u" negatable:"" help:"Only update existing change requests, do not create new ones"`

	// DraftDefault is used to set the default draft value
	// when creating new Change Requests.
	//
	// --draft/--no-draft will override this value.
	DraftDefault bool `config:"submit.draft" help:"Default value for --draft when creating change requests." hidden:"" default:"false"`

	// TODO: Other creation options e.g.:
	// - milestone
	// - reviewers

	Labels           []string `name:"label" short:"l" help:"Add labels to the change request. Pass multiple times or separate with commas."`
	ConfiguredLabels []string `name:"configured-labels" help:"Default labels to add to change requests." hidden:"" config:"submit.label"` // merged with Labels

	Reviewers           []string `short:"r" name:"reviewer" help:"Add reviewers to the change request. Pass multiple times or separate with commas." released:"unreleased"`
	ConfiguredReviewers []string `name:"configured-reviewers" help:"Default reviewers to add to change requests." hidden:"" config:"submit.reviewers" released:"unreleased"` // merged with Reviewers

	Assignees           []string `short:"a" name:"assign" placeholder:"ASSIGNEE" help:"Assign the change request to these users. Pass multiple times or separate with commas." released:"unreleased"`
	ConfiguredAssignees []string `name:"configured-assignees" help:"Default assignees to add to change requests." hidden:"" config:"submit.assignees" released:"unreleased"` // merged with Assignees

	// ListTemplatesTimeout controls the timeout for listing CR templates.
	ListTemplatesTimeout time.Duration `hidden:"" config:"submit.listTemplatesTimeout" help:"Timeout for listing CR templates" default:"1s"`

	// Template specifies the template to use when multiple templates are available.
	// If set, this template will be automatically selected instead of prompting the user.
	// The value should match the filename of one of the available templates.
	Template string `hidden:"" config:"submit.template" help:"Default template to use when multiple templates are available"`
}

func mergeConfiguredValues(values []string, configured []string) []string {
	return slices.Collect(iterutil.Uniq(values, configured))
}

func mergeConfiguredOptions(opts *Options) {
	opts.Labels = mergeConfiguredValues(opts.Labels, opts.ConfiguredLabels)
	opts.Reviewers = mergeConfiguredValues(opts.Reviewers, opts.ConfiguredReviewers)
	opts.Assignees = mergeConfiguredValues(opts.Assignees, opts.ConfiguredAssignees)
}

// NavCommentSync specifies the scope of navigation comment updates.
type NavCommentSync int

const (
	// NavCommentSyncBranch updates navigation comments
	// only for branches that are being submitted.
	//
	// This is the default.
	NavCommentSyncBranch NavCommentSync = iota

	// NavCommentSyncDownstack updates navigation comments
	// for all submitted branches and their downstack branches.
	NavCommentSyncDownstack
)

var _ encoding.TextUnmarshaler = (*NavCommentSync)(nil)

// String returns the string representation of the NavCommentSync.
func (s NavCommentSync) String() string {
	switch s {
	case NavCommentSyncBranch:
		return "branch"
	case NavCommentSyncDownstack:
		return "downstack"
	default:
		return "unknown"
	}
}

// UnmarshalText decodes a NavCommentSync from text.
// It supports "branch" and "downstack" values.
func (s *NavCommentSync) UnmarshalText(bs []byte) error {
	switch string(bs) {
	case "branch":
		*s = NavCommentSyncBranch
	case "downstack":
		*s = NavCommentSyncDownstack
	default:
		return fmt.Errorf("invalid value %q: expected branch or downstack", bs)
	}
	return nil
}

// NavCommentDownstack specifies which downstack CRs
// to include in navigation comments.
type NavCommentDownstack int

const (
	// NavCommentDownstackAll includes all downstack CRs
	// (both open and merged).
	//
	// This is the default.
	NavCommentDownstackAll NavCommentDownstack = iota

	// NavCommentDownstackOpen includes only open downstack CRs,
	// excluding merged ones.
	NavCommentDownstackOpen
)

var _ encoding.TextUnmarshaler = (*NavCommentDownstack)(nil)

// String returns the string representation of the NavCommentDownstack.
func (d NavCommentDownstack) String() string {
	switch d {
	case NavCommentDownstackAll:
		return "all"
	case NavCommentDownstackOpen:
		return "open"
	default:
		return "unknown"
	}
}

// UnmarshalText decodes NavCommentDownstack from text.
func (d *NavCommentDownstack) UnmarshalText(bs []byte) error {
	switch string(bs) {
	case "all":
		*d = NavCommentDownstackAll
	case "open":
		*d = NavCommentDownstackOpen
	default:
		return fmt.Errorf("invalid value %q: expected all or open", bs)
	}
	return nil
}

// BatchOptions defines options
// that are only available to batch submit operations.
type BatchOptions struct {
	UpdateOnlyDefault bool `config:"submit.updateOnly" help:"Default value for --update-only in batch submit operations." hidden:"" default:"false"`
}

// BatchRequest is a request to submit one or more change requests.
type BatchRequest struct {
	Branches     []string // required
	Options      *Options
	BatchOptions *BatchOptions // required
}

// SubmitBatch submits a batch of branches to a remote repository,
// creating or updating change requests as needed.
func (h *Handler) SubmitBatch(ctx context.Context, req *BatchRequest) error {
	opts := cmp.Or(req.Options, &Options{})
	mergeConfiguredOptions(opts)

	batchOpts := cmp.Or(req.BatchOptions, &BatchOptions{})
	if batchOpts.UpdateOnlyDefault && opts.UpdateOnly == nil {
		// If the user didn't specify --update-only flag,
		// use the default value from the config.
		opts.UpdateOnly = &batchOpts.UpdateOnlyDefault
	}

	var branchesToComment []string
	for _, branch := range req.Branches {
		// Shallow copy the options because submitBranch may modify them.
		opts := *opts

		status, err := h.submitBranch(
			ctx,
			branch,
			&submitOptions{Options: &opts},
		)
		if err != nil {
			return fmt.Errorf("submit branch %s: %w", branch, err)
		}
		if status.Submitted {
			branchesToComment = append(branchesToComment, branch)
		}
	}

	if len(branchesToComment) == 0 || opts.DryRun {
		return nil // nothing to do
	}

	return updateNavigationComments(
		ctx,
		h.Store, h.Service, h.Log,
		opts.NavComment,
		opts.NavCommentSync,
		opts.NavCommentDownstack,
		opts.NavCommentMarker,
		branchesToComment,
		h.RemoteRepository,
	)
}

// Request is a request to submit a single branch to a remote repository.
type Request struct {
	// Branch is the name of the branch to submit.
	Branch string // required

	// Title and Body are the title and body of the change request.
	Title, Body string // optional

	// Options are the options for the submit operation.
	Options *Options // optional
}

// Submit submits a single branch to a remote repository,
// creating or updating a change request as needed.
func (h *Handler) Submit(ctx context.Context, req *Request) error {
	opts := cmp.Or(req.Options, &Options{})
	mergeConfiguredOptions(opts)
	status, err := h.submitBranch(
		ctx,
		req.Branch,
		&submitOptions{
			Options: opts,
			Title:   req.Title,
			Body:    req.Body,
		},
	)
	if err != nil {
		return fmt.Errorf("submit branch %s: %w", req.Branch, err)
	}

	if !status.Submitted || opts.DryRun {
		// Nothing was submitted, so nothing to do.
		return nil
	}

	return updateNavigationComments(
		ctx,
		h.Store, h.Service, h.Log,
		opts.NavComment,
		opts.NavCommentSync,
		opts.NavCommentDownstack,
		opts.NavCommentMarker,
		[]string{req.Branch},
		h.RemoteRepository,
	)
}

type submitStatus struct {
	// Submitted indicates whether the branch was actually submitted
	// to a Forge.
	//
	// If yes, comments will be added or updated
	// based on the NavComment option.
	Submitted bool
}

type submitOptions struct {
	*Options

	Title, Body string
}

func (h *Handler) submitBranch(
	ctx context.Context,
	branchToSubmit string,
	opts *submitOptions,
) (status submitStatus, err error) {
	if branchToSubmit == h.Store.Trunk() {
		return status, errors.New("cannot submit trunk")
	}

	svc := h.Service
	log := h.Log

	// Refuse to submit if the branch is not restacked.
	if !opts.Force {
		if err := svc.VerifyRestacked(ctx, branchToSubmit); err != nil {
			log.Errorf("Branch %s needs to be restacked.", branchToSubmit)
			log.Errorf("Run the following command to fix this:")
			log.Errorf("  gs branch restack --branch=%s", branchToSubmit)
			log.Errorf("Or, try again with --force to submit anyway.")
			return status, errors.New("refusing to submit outdated branch")
		}
	}

	branch, err := svc.LookupBranch(ctx, branchToSubmit)
	if err != nil {
		return status, fmt.Errorf("lookup branch: %w", err)
	}

	// Various code paths down below should call this
	// if the branch is being published as a CR (new or existing)
	// so it should get a nav comment.
	var _needsNavCommentOnce sync.Once
	needsNavComment := func() {
		_needsNavCommentOnce.Do(func() {
			status.Submitted = true
		})
	}

	commitHash, err := h.Repository.PeelToCommit(ctx, branchToSubmit)
	if err != nil {
		return status, fmt.Errorf("peel to commit: %w", err)
	}

	remote, err := h.Remote(ctx)
	if err != nil {
		return status, fmt.Errorf("get remote: %w", err)
	}

	// TODO:
	// Encapsulate (localBranch, upstreamBranch) in a struct.

	// Prefer the upstream branch name stored in the data store if available.
	// This is how we account for branches that have been renamed after submitting.
	upstreamBranch := branch.UpstreamBranch
	if upstreamBranch == "" {
		// If the branch doesn't have an upstream branch name,
		// but has been manually pushed with an upstream branch name
		// to the same remote, use that.
		if upstream, err := h.Repository.BranchUpstream(ctx, branchToSubmit); err == nil {
			// origin/branch -> branch
			if b, ok := strings.CutPrefix(upstream, remote+"/"); ok {
				upstreamBranch = b
				log.Infof("%v: Using upstream name '%v'", branchToSubmit, upstreamBranch)
				log.Infof("%v: If this is incorrect, cancel this operation and run 'git branch --unset-upstream %v'.", branchToSubmit, branchToSubmit)
			}
		}
	}

	// Similarly, if the branch's base has a different name upstream,
	// use that name instead.
	upstreamBase := branch.Base
	if branch.Base != h.Store.Trunk() {
		baseBranch, err := svc.LookupBranch(ctx, branch.Base)
		if err != nil {
			return status, fmt.Errorf("lookup base branch: %w", err)
		}
		upstreamBase = cmp.Or(baseBranch.UpstreamBranch, branch.Base)
	}

	var existingChange *forge.FindChangeItem
	if branch.Change == nil && opts.Publish {
		// If the branch doesn't have a CR associated with it,
		// we'll probably need to create one,
		// but verify that there isn't already one open.
		// If the branch doesn't have a CR associated with it,
		// we'll probably need to create one,
		// but verify that there isn't already one open.
		remoteRepo, err := h.RemoteRepository(ctx)
		if err != nil {
			return status, fmt.Errorf("discover CR for %s: %w", branchToSubmit, err)
		}

		// Search for a CR associated with the branch's upstream branch
		// or the branch name itself if we don't have an upstream branch.
		// In case of the latter, we'll need to verify that the HEAD matches.
		crBranch := cmp.Or(upstreamBranch, branchToSubmit)
		changes, err := remoteRepo.FindChangesByBranch(ctx, crBranch, forge.FindChangesOptions{
			State: forge.ChangeOpen,
			Limit: 3,
		})
		if err != nil {
			return status, fmt.Errorf("list changes: %w", err)
		}

		switch len(changes) {
		case 0:
			// No PRs found, one will be created later.

		case 1:
			// If matching by local branch name, verify that the HEAD matches.
			// If not, pretend we didn't find a matching CR.
			if upstreamBranch == "" {
				change := changes[0]
				if change.HeadHash != commitHash {
					log.Infof("%v: Ignoring CR %v with the same branch name: remote HEAD (%v) does not match local HEAD (%v)",
						branchToSubmit, change.ID, change.HeadHash, commitHash)
					log.Infof("%v: If this is incorrect, cancel this operation, 'git pull' the branch, and retry.", branchToSubmit)
					break
				}
				upstreamBranch = branchToSubmit
			}

			// A CR was found, but it wasn't associated with the branch.
			// It was probably created manually.
			// We'll associate it now.
			existingChange = changes[0]
			log.Infof("%v: Found existing CR %v", branchToSubmit, existingChange.ID)

			md, err := remoteRepo.NewChangeMetadata(ctx, existingChange.ID)
			if err != nil {
				return status, fmt.Errorf("get change metadata: %w", err)
			}

			// If we're importing an existing CR,
			// also check if there's a stack navigation comment to import.
			listCommentOpts := forge.ListChangeCommentsOptions{
				BodyMatchesAll: _navCommentRegexes,
				CanUpdate:      true,
			}

			for comment, err := range remoteRepo.ListChangeComments(ctx, existingChange.ID, &listCommentOpts) {
				if err != nil {
					log.Warn("Could not list comments for CR. Ignoring existing comments.", "cr", existingChange.ID, "error", err)
					break
				}

				log.Infof("%v: Found existing navigation comment: %v", branchToSubmit, comment.ID)
				md.SetNavigationCommentID(comment.ID)
				break
			}

			// TODO: this should all happen in Service, probably.
			changeMeta, err := remoteRepo.Forge().MarshalChangeMetadata(md)
			if err != nil {
				return status, fmt.Errorf("marshal change metadata: %w", err)
			}

			tx := h.Store.BeginBranchTx()
			msg := fmt.Sprintf("%v: associate existing CR", branchToSubmit)
			if err := tx.Upsert(ctx, state.UpsertRequest{
				Name:           branchToSubmit,
				ChangeForge:    md.ForgeID(),
				ChangeMetadata: changeMeta,
				UpstreamBranch: &upstreamBranch,
			}); err != nil {
				return status, fmt.Errorf("%s: %w", msg, err)
			}

			if err := tx.Commit(ctx, msg); err != nil {
				return status, fmt.Errorf("update state: %w", err)
			}

		default:
			// GitHub doesn't allow multiple PRs for the same branch
			// with the same base branch.
			// If we get here, it means there are multiple PRs open
			// with different base branches.
			return status, fmt.Errorf("multiple open change requests for %s", branchToSubmit)
			// TODO: Ask the user to pick one and associate it with the branch.
		}
	} else if branch.Change != nil {
		remoteRepo, err := h.RemoteRepository(ctx)
		if err != nil {
			return status, fmt.Errorf("look up CR %v: %w", branch.Change.ChangeID(), err)
		}

		// If a CR is already associated with the branch,
		// fetch information about it to compare with the current state.
		change, err := remoteRepo.FindChangeByID(ctx, branch.Change.ChangeID())
		if err != nil {
			return status, fmt.Errorf("find change: %w", err)
		}

		// Consider the CR only if it's open.
		if change.State == forge.ChangeOpen {
			existingChange = change
		} else {
			var state string
			if change.State == forge.ChangeMerged {
				state = "merged"
			} else {
				state = "closed"
			}

			log.Infof("%v: Ignoring CR %v as it was %s: %v", branchToSubmit, change.ID, state, change.URL)
			// TODO:
			// We could offer to reopen the CR if it was closed,
			// but not if it was merged.
		}
	}

	var openURL string
	if !opts.DryRun && opts.Web.shouldOpen(existingChange == nil /* new CR */) {
		defer func() {
			if openURL == "" {
				return
			}
			if err := h.Browser.OpenURL(openURL); err != nil {
				log.Warn("Could not open browser",
					"url", openURL,
					"error", err)
			}
		}()
	}

	// At this point, existingChange is nil only if we need to create a new CR.
	if existingChange == nil {
		if upstreamBranch == "" {
			unique, err := svc.UnusedBranchName(ctx, remote, branchToSubmit)
			if err != nil {
				return status, fmt.Errorf("find unique branch name: %w", err)
			}

			if unique != branchToSubmit {
				log.Infof("%v: Branch name already in use in remote '%v'", branchToSubmit, remote)
				log.Infof("%v: Using upstream name '%v' instead", branchToSubmit, unique)
			}
			upstreamBranch = unique
		}

		if opts.UpdateOnly != nil && *opts.UpdateOnly {
			if !opts.DryRun {
				// TODO: config to disable this message?
				log.Infof("%v: Skipping unsubmitted branch: --update-only", branchToSubmit)
			}
			return status, nil
		}

		if opts.DryRun {
			if opts.Publish {
				log.Infof("WOULD create a CR for %s", branchToSubmit)
			} else {
				log.Infof("WOULD push branch %s", branchToSubmit)
			}
			return status, nil
		}

		// Sanity check:
		// If we're going to push the branch,
		// make sure that the fetch refspec will actually fetch it.
		// Otherwise, we will push to origin/feature,
		// but won't have a local refs/remotes/origin/feature
		// to track it after a 'git fetch'.
		if refspecs, err := h.Repository.RemoteFetchRefspecs(ctx, remote); err != nil {
			log.Warn("Unable to verify remote's fetch refspecs",
				"remote", remote,
				"error", err)
		} else {
			wantMatch := "refs/heads/" + upstreamBranch
			var hasMatch bool
			for _, refspec := range refspecs {
				if refspec.Matches(wantMatch) {
					hasMatch = true
					break
				}
			}

			if !hasMatch && !opts.Force {
				log.Errorf("Remote '%v' has refspecs:", remote)
				for _, refspec := range refspecs {
					log.Errorf("  - %v", refspec)
				}
				user := cmp.Or(os.Getenv("USER"), "yourname")
				log.Errorf("None of these will fetch branch '%v' after pushing.", upstreamBranch)
				log.Error("This will make follow up changes on them impossible.")
				log.Error("To fix this, you can do one of the following:")
				log.Errorf("1. Manually add a fetch refspec for just this branch:")
				log.Errorf("       git config --add remote.%v.fetch +refs/heads/%v:refs/remotes/%v/%v",
					remote, upstreamBranch, remote, upstreamBranch)
				log.Errorf("2. Prefix all your branches with your username (e.g. '%v/%v'),", user, upstreamBranch)
				log.Errorf("   and add a fetch refspec to fetch all branches under that prefix:")
				log.Errorf("       git config --add remote.%v.fetch '+refs/heads/%v/*:refs/remotes/%v/%v/*'",
					remote, user, remote, user)
				log.Errorf("   You can configure git-spice to automatically add this prefix for future branches with:")
				log.Errorf("       git config --global spice.branchCreate.prefix %v/", user)
				log.Errorf("3. Use the --force flag to push anyway (not recommended).")
				return status, errors.New("remote cannot fetch pushed branch")
			}
		}

		var prepared *preparedBranch
		if opts.Publish {
			needsNavComment()

			remoteRepo, err := h.RemoteRepository(ctx)
			if err != nil {
				return status, fmt.Errorf("prepare publish: %w", err)
			}

			// TODO: Refactor:
			// NoPublish and DryRun are checked repeatedly.
			// Extract the logic that needs them into no-ops
			// and make this function flow more linearly.
			prepared, err = h.prepareBranch(
				ctx,
				branchToSubmit,
				remote, // TODO: need this?
				remoteRepo,
				upstreamBranch, branch.Base, upstreamBase,
				opts,
			)
			if err != nil {
				return status, err
			}
		}

		pushOpts := git.PushOptions{
			Remote: remote,
			Refspec: git.Refspec(
				commitHash.String() + ":refs/heads/" + upstreamBranch,
			),
			Force:    opts.Force,
			NoVerify: opts.NoVerify,
		}

		// If we've already pushed this branch before,
		// we'll need a force push.
		// Use a --force-with-lease to avoid
		// overwriting someone else's changes.
		if !opts.Force {
			existingHash, err := h.Repository.PeelToCommit(ctx, remote+"/"+upstreamBranch)
			if err == nil {
				pushOpts.ForceWithLease = upstreamBranch + ":" + existingHash.String()
			}
		}

		err = h.Worktree.Push(ctx, pushOpts)
		if err != nil {
			return status, fmt.Errorf("push branch: %w", err)
		}

		// At this point, even if any other operation fails,
		// we need to save to the state that we pushed the branch
		// with the recorded name.
		upsert := state.UpsertRequest{
			Name:           branchToSubmit,
			UpstreamBranch: &upstreamBranch,
		}
		defer func() {
			msg := "branch submit " + branchToSubmit
			tx := h.Store.BeginBranchTx()
			err := errors.Join(
				tx.Upsert(ctx, upsert),
				tx.Commit(ctx, msg),
			)
			if err != nil {
				log.Warn("Could not update branch state",
					"branch", branchToSubmit,
					"error", err)
				return
			}
		}()

		upstream := remote + "/" + upstreamBranch
		if err := h.Repository.SetBranchUpstream(ctx, branchToSubmit, upstream); err != nil {
			log.Warn("Could not set upstream", "branch", branchToSubmit, "remote", remote, "error", err)
		}

		if prepared != nil {
			changeID, changeURL, err := prepared.Publish(ctx)
			if err != nil {
				return status, fmt.Errorf("publish change: %w", err)
			}
			openURL = changeURL

			remoteRepo := prepared.remoteRepo
			changeMeta, err := remoteRepo.NewChangeMetadata(ctx, changeID)
			if err != nil {
				return status, fmt.Errorf("get change metadata: %w", err)
			}

			changeIDJSON, err := remoteRepo.Forge().MarshalChangeMetadata(changeMeta)
			if err != nil {
				return status, fmt.Errorf("marshal change ID: %w", err)
			}

			upsert.ChangeForge = changeMeta.ForgeID()
			upsert.ChangeMetadata = changeIDJSON
		} else {
			// no-publish mode, so no CR was created.
			log.Infof("Pushed %s", branchToSubmit)
		}
	} else {
		needsNavComment()
		if upstreamBranch == "" {
			log.Error("No upstream branch was found for branch %v with existing CR %v", branchToSubmit, existingChange.ID)
			log.Error("We cannot update the CR without an upstream branch name.")
			log.Error("To fix this, identify the correct upstream branch name and set it with, e.g.:")
			log.Error("  git branch --set-upstream-to=<remote>/<branch> %v", branchToSubmit)
			log.Error("Then, try again.")
			return status, errors.New("upstream branch not set")
		}

		if !opts.Publish {
			log.Warnf("Ignoring --no-publish: %s was already published: %s", branchToSubmit, existingChange.URL)
		}

		// Check base and HEAD are up-to-date.
		pull := existingChange
		openURL = pull.URL
		var updates []string
		if pull.HeadHash != commitHash {
			updates = append(updates, "push branch")
		}
		if pull.BaseName != upstreamBase {
			updates = append(updates, "set base to "+upstreamBase)
		}
		if opts.Draft != nil && pull.Draft != *opts.Draft {
			updates = append(updates, "set draft to "+strconv.FormatBool(*opts.Draft))
		}

		if len(opts.Assignees) > 0 {
			existingAssigneeSet := make(map[string]struct{}, len(pull.Assignees))
			for _, assignee := range pull.Assignees {
				existingAssigneeSet[assignee] = struct{}{}
			}

			var assigneesToAdd []string
			for _, assignee := range opts.Assignees {
				if _, exists := existingAssigneeSet[assignee]; !exists {
					assigneesToAdd = append(assigneesToAdd, assignee)
				}
			}
			if len(assigneesToAdd) > 0 {
				sort.Strings(assigneesToAdd)
				updates = append(updates, "add assignees: "+strings.Join(assigneesToAdd, ", "))
			}
		}

		// Check for labels that would be added.
		if len(opts.Labels) > 0 {
			existingLabelSet := make(map[string]struct{}, len(pull.Labels))
			for _, label := range pull.Labels {
				existingLabelSet[label] = struct{}{}
			}
			var labelsToAdd []string
			for _, label := range opts.Labels {
				if _, exists := existingLabelSet[label]; !exists {
					labelsToAdd = append(labelsToAdd, label)
				}
			}
			if len(labelsToAdd) > 0 {
				sort.Strings(labelsToAdd)
				updates = append(updates, "add labels: "+strings.Join(labelsToAdd, ", "))
			}
		}

		// Check for reviewers that would be added.
		if len(opts.Reviewers) > 0 {
			existingReviewerSet := make(map[string]struct{}, len(pull.Reviewers))
			for _, reviewer := range pull.Reviewers {
				existingReviewerSet[reviewer] = struct{}{}
			}
			var reviewersToAdd []string
			for _, reviewer := range opts.Reviewers {
				if _, exists := existingReviewerSet[reviewer]; !exists {
					reviewersToAdd = append(reviewersToAdd, reviewer)
				}
			}
			if len(reviewersToAdd) > 0 {
				sort.Strings(reviewersToAdd)
				updates = append(updates, "add reviewers: "+strings.Join(reviewersToAdd, ", "))
			}
		}

		if len(updates) == 0 {
			log.Infof("CR %v is up-to-date: %s", pull.ID, pull.URL)
			return status, nil
		}

		if opts.DryRun {
			log.Infof("WOULD update CR %v:", pull.ID)
			for _, update := range updates {
				log.Infof("  - %s", update)
			}
			return status, nil
		}

		if pull.HeadHash != commitHash {
			pushOpts := git.PushOptions{
				Remote: remote,
				Refspec: git.Refspec(
					commitHash.String() + ":refs/heads/" + upstreamBranch,
				),
				Force:    opts.Force,
				NoVerify: opts.NoVerify,
			}
			if !opts.Force {
				// Force push, but only if the ref is exactly
				// where we think it is.
				existingHash, err := h.Repository.PeelToCommit(ctx, remote+"/"+upstreamBranch)
				if err == nil {
					pushOpts.ForceWithLease = upstreamBranch + ":" + existingHash.String()
				}
			}

			if err := h.Worktree.Push(ctx, pushOpts); err != nil {
				log.Error("Push failed. Branch may have been updated by someone else. Try with --force.")
				return status, fmt.Errorf("push branch: %w", err)
			}
		}

		if len(updates) > 0 {
			opts := forge.EditChangeOptions{
				Base:      upstreamBase,
				Draft:     opts.Draft,
				Labels:    opts.Labels,
				Reviewers: opts.Reviewers,
				Assignees: opts.Assignees,
			}

			// remoteRepo is guaranteed to be available at this point.
			remoteRepo, err := h.RemoteRepository(ctx)
			if err != nil {
				return status, fmt.Errorf("edit CR %v: %w", pull.ID, err)
			}

			if err := remoteRepo.EditChange(ctx, pull.ID, opts); err != nil {
				return status, fmt.Errorf("edit CR %v: %w", pull.ID, err)
			}
		}

		log.Infof("Updated %v: %s", pull.ID, pull.URL)
	}

	return status, nil
}

func (h *Handler) prepareBranch(
	ctx context.Context,
	branchToSubmit string,
	remoteName string,
	remoteRepo forge.Repository,
	upstreamBranch, baseBranch, upstreamBase string,
	opts *submitOptions,
) (*preparedBranch, error) {
	// Fetch the template while we're prompting the other fields.
	changeTemplatesCh := make(chan []*forge.ChangeTemplate, 1)
	go func() {
		defer close(changeTemplatesCh)

		changeTemplatesCh <- listChangeTemplates(ctx, h.Service, h.Log, remoteName, remoteRepo, opts.Options)
	}()

	msgs, err := h.Repository.CommitMessageRange(ctx, branchToSubmit, baseBranch)
	if err != nil {
		return nil, fmt.Errorf("list commits: %w", err)
	}

	// Check if the branch has the same tree as its base.
	branchTree, err := h.Repository.PeelToTree(ctx, branchToSubmit)
	if err != nil {
		return nil, fmt.Errorf("get branch tree: %w", err)
	}
	baseTree, err := h.Repository.PeelToTree(ctx, baseBranch)
	if err != nil {
		return nil, fmt.Errorf("get base tree: %w", err)
	}

	if branchTree == baseTree {
		if !ui.Interactive(h.View) {
			h.Log.Warnf("Branch %s has no changes compared to its base (%s).", branchToSubmit, baseBranch)
			h.Log.Warnf("Submitting it will create an empty change request.")
		} else {
			var submitNoChanges bool
			field := ui.NewConfirm().
				WithTitle("Submit branch with no changes?").
				WithDescription(
					fmt.Sprintf("Branch %s has no changes compared to its base (%s). "+
						"Submitting it will create an empty change request. "+
						"This is usually not what you want to do.", branchToSubmit, baseBranch)).
				WithValue(&submitNoChanges)
			if err := ui.Run(h.View, field); err != nil {
				return nil, fmt.Errorf("run prompt: %w", err)
			}
			if !submitNoChanges {
				return nil, errors.New("operation aborted")
			}
		}
	}

	var (
		defaultTitle string
		defaultBody  strings.Builder
	)
	switch len(msgs) {
	case 0:
		// No commits, use branch name as default title.
		defaultTitle = branchToSubmit

	case 1:
		// If there's only one commit,
		// just the body will be the default body.
		defaultTitle = msgs[0].Subject
		defaultBody.WriteString(msgs[0].Body)

	default:
		// Otherwise, we'll concatenate all the messages.
		// The revisions are in reverse order,
		// so we'll want to iterate in reverse.
		defaultTitle = msgs[len(msgs)-1].Subject
		for i := len(msgs) - 1; i >= 0; i-- {
			msg := msgs[i]
			if defaultBody.Len() > 0 {
				defaultBody.WriteString("\n\n")
			}
			defaultBody.WriteString(msg.Subject)
			if msg.Body != "" {
				defaultBody.WriteString("\n\n")
				defaultBody.WriteString(msg.Body)
			}
		}
	}

	var fields []ui.Field
	form := newBranchSubmitForm(ctx, h.Service, h.Repository, remoteRepo, h.Log, opts.Options)
	if opts.Title == "" {
		opts.Title = defaultTitle
		fields = append(fields, form.titleField(&opts.Title, msgs))
	}

	if opts.Body == "" {
		opts.Body = defaultBody.String()
		if opts.Fill {
			// If the user selected --fill,
			// and there are templates to choose from,
			// just pick the first template in the body.
			tmpls := <-changeTemplatesCh
			if len(tmpls) > 0 {
				opts.Body += "\n\n" + tmpls[0].Body
			}
		} else {
			// Otherwise, we'll prompt for the template (if needed)
			// and the body.
			fields = append(fields, form.templateField(changeTemplatesCh))
			fields = append(fields, form.bodyField(&opts.Body))
		}
	}

	// Don't mess with draft setting if we're not prompting
	// and the user didn't explicitly set it.
	if ui.Interactive(h.View) && opts.Draft == nil {
		draftDefault := opts.DraftDefault
		opts.Draft = &draftDefault
		fields = append(fields, form.draftField(opts.Draft))
	}

	// TODO: should we assume --fill if --no-prompt?
	if len(fields) > 0 && !opts.Fill {
		if !ui.Interactive(h.View) {
			return nil, fmt.Errorf("prompt for commit information: %w", ui.ErrPrompt)
		}

		// If we're prompting and there's a prior submission attempt,
		// change the title and body to the saved values.
		prePrepared, err := h.Store.LoadPreparedBranch(ctx, branchToSubmit)
		if err == nil && prePrepared != nil {
			usePrepared := true
			f := ui.NewConfirm().
				WithValue(&usePrepared).
				WithTitle("Recover previously filled information?").
				WithDescription(
					"We found previously filled information for this branch.\n" +
						"Would you like to recover and edit it?")
			if err := ui.Run(h.View, f); err != nil {
				return nil, fmt.Errorf("prompt for recovery: %w", err)
			}

			if usePrepared {
				opts.Title = prePrepared.Subject
				opts.Body = prePrepared.Body
			} else {
				// It will get cleared anyway when the branch
				// is submitted, but clear it now to avoid the
				// prompt again if this submission also fails.
				if err := h.Store.ClearPreparedBranch(ctx, branchToSubmit); err != nil {
					h.Log.Warn("Could not clear prepared branch information", "error", err)
				}
			}
		}

		if err := ui.Run(h.View, fields...); err != nil {
			return nil, fmt.Errorf("prompt form: %w", err)
		}
	}
	must.NotBeBlankf(opts.Title, "CR title must have been set")

	storePrepared := state.PreparedBranch{
		Name:    branchToSubmit,
		Subject: opts.Title,
		Body:    opts.Body,
	}

	draft := opts.DraftDefault
	if opts.Draft != nil {
		draft = *opts.Draft
	}

	if err := h.Store.SavePreparedBranch(ctx, &storePrepared); err != nil {
		h.Log.Warn("Could not save prepared branch. Will be unable to recover CR metadata if the push fails.", "error", err)
	}

	return &preparedBranch{
		PreparedBranch: storePrepared,
		draft:          draft,
		head:           upstreamBranch,
		base:           upstreamBase,
		remoteRepo:     remoteRepo,
		store:          h.Store,
		log:            h.Log,
		labels:         opts.Labels,
		reviewers:      opts.Reviewers,
		assignees:      opts.Assignees,
	}, nil
}

func listChangeTemplates(
	ctx context.Context,
	svc Service,
	log *silog.Logger,
	remoteName string,
	remoteRepo forge.Repository,
	opts *Options,
) []*forge.ChangeTemplate {
	if opts.ListTemplatesTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.ListTemplatesTimeout)
		defer cancel()
	}

	templates, err := svc.ListChangeTemplates(ctx, remoteName, remoteRepo)
	if err != nil {
		log.Warn("Could not list change templates", "error", err)
		return nil
	}

	slices.SortFunc(templates, func(a, b *forge.ChangeTemplate) int {
		return strings.Compare(a.Filename, b.Filename)
	})

	return templates
}

// preparedBranch is a branch that is ready to be published as a CR
// (or equivalent).
type preparedBranch struct {
	state.PreparedBranch

	head      string
	base      string
	draft     bool
	labels    []string
	reviewers []string
	assignees []string

	remoteRepo forge.Repository
	store      Store
	log        *silog.Logger
}

func (b *preparedBranch) Publish(ctx context.Context) (forge.ChangeID, string, error) {
	result, err := b.remoteRepo.SubmitChange(ctx, forge.SubmitChangeRequest{
		Subject:   b.Subject,
		Body:      b.Body,
		Head:      b.head,
		Base:      b.base,
		Draft:     b.draft,
		Labels:    b.labels,
		Reviewers: b.reviewers,
		Assignees: b.assignees,
	})
	if err != nil {
		// If the branch could not be submitted because the base branch
		// has not been pushed yet, provide a more user-friendly error.
		if errors.Is(err, forge.ErrUnsubmittedBase) {
			b.log.Errorf("%v: cannot be submitted because base branch %q does not exist in the remote.", b.Name, b.base)
			b.log.Errorf("Try submitting the base branch first:")
			b.log.Errorf("  gs branch submit --branch=%s", b.base)
			return nil, "", fmt.Errorf("create change: %w", err)
		}
		return nil, "", fmt.Errorf("create change: %w", err)
	}

	if err := b.store.ClearPreparedBranch(ctx, b.Name); err != nil {
		b.log.Warn("Could not clear prepared branch", "error", err)
	}

	b.log.Infof("Created %v: %s", result.ID, result.URL)
	return result.ID, result.URL, nil
}

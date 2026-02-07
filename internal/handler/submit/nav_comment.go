package submit

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/stacknav"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
)

// NavCommentWhen specifies when a navigation comment should be posted
// (or updated if it already exists).
type NavCommentWhen int

const (
	// NavCommentAlways always posts a navigation comment.
	// This is the default.
	NavCommentAlways NavCommentWhen = iota

	// NavCommentNever disables posting navigation comments.
	// If an existing comment is found, it is left as is.
	NavCommentNever

	// NavCommentOnMultiple posts a navigation comment
	// only if there are multiple branches in the stack
	// that the current branch is part of.
	NavCommentOnMultiple
)

var _ encoding.TextUnmarshaler = (*NavCommentWhen)(nil)

// UnmarshalText decodes a NavCommentWhen from text.
// It supports "true", "false", and "multiple" values.
func (f *NavCommentWhen) UnmarshalText(bs []byte) error {
	switch string(bs) {
	case "true", "1", "yes":
		*f = NavCommentAlways
	case "false", "0", "no":
		*f = NavCommentNever
	case "multiple":
		*f = NavCommentOnMultiple
	default:
		return fmt.Errorf("invalid value %q: expected true, false, or multiple", bs)
	}
	return nil
}

func (f NavCommentWhen) String() string {
	switch f {
	case NavCommentAlways:
		return "true"
	case NavCommentNever:
		return "false"
	case NavCommentOnMultiple:
		return "multiple"
	default:
		return "unknown"
	}
}

// For each branch in the list of submitted branches,
// we'll add or update a comment in the form:
//
//	This change is part of the following stack:
//
//	- #123
//	  - #124 ◀
//	    - #125
//
//	<sub>Change managed by [git-spice](https://abhinav.github.io/git-spice/).</sub>
//
// Where the arrow indicates the current branch.
// For cases where this is the first time we're posting the comment,
// we'll need to also update the store to record the comment ID for later.
func updateNavigationComments(
	ctx context.Context,
	store Store,
	svc Service,
	log *silog.Logger,
	navComment NavCommentWhen,
	navCommentSync NavCommentSync,
	navCommentDownstack NavCommentDownstack,
	navCommentMarker string,
	submittedBranches []string,
	getRemoteRepo func(context.Context) (forge.Repository, error),
) error {
	if len(submittedBranches) == 0 {
		return nil
	}

	if navComment == NavCommentNever {
		return nil // nothing to do
	}

	remoteRepo, err := getRemoteRepo(ctx)
	if err != nil {
		return fmt.Errorf("get remote repository: %w", err)
	}

	// Create URL formatter for forges that need explicit links.
	// Forges like GitHub auto-link "#123" to PRs, but Bitbucket doesn't.
	var urlFormatter func(forge.ChangeID) string
	if repo, ok := remoteRepo.(forge.WithChangeURL); ok {
		urlFormatter = func(id forge.ChangeID) string {
			return fmt.Sprintf("[%s](%s)", id.String(), repo.ChangeURL(id))
		}
	}

	// Look up branch graph once, and share between all syncs.
	trackedBranches, err := svc.LoadBranches(ctx)
	if err != nil {
		return fmt.Errorf("list tracked branches: %w", err)
	}

	type branchInfo struct {
		Branch string
		Meta   forge.ChangeMetadata
	}

	var (
		nodes []*stackedChange
		infos []branchInfo // info for nodes[i]
	)
	idxByBranch := make(map[string]int) // branch -> index in nodes

	// First pass: add nodes but don't connect.
	for _, b := range trackedBranches {
		if b.Change == nil {
			continue
		}

		idxByBranch[b.Name] = len(nodes)
		nodes = append(nodes, &stackedChange{
			Change:       b.Change.ChangeID(),
			Base:         -1,
			urlFormatter: urlFormatter,
		})
		infos = append(infos, branchInfo{
			Branch: b.Name,
			Meta:   b.Change,
		})
	}

	// Second pass:
	//
	// - Add merged downstacks as separate nodes (if enabled).
	// - Connect Aboves if this is a base to another node.
	for _, b := range trackedBranches {
		nodeIdx, ok := idxByBranch[b.Name]
		if !ok {
			continue
		}

		// Add nodes starting at the bottom.
		// For each merged downstack branch:
		//
		//  - previous branch is the base (starting at trunk)
		//  - current branch is added to Aboves of previous branch
		//
		// Skip merged downstack branches if navCommentDownstack is Open.
		lastDownstackIdx := -1
		if navCommentDownstack == NavCommentDownstackAll {
			for _, crJSON := range b.MergedDownstack {
				downstackCR, err := remoteRepo.Forge().UnmarshalChangeID(crJSON)
				if err != nil {
					log.Warn("Skiping invalid downstack change",
						"branch", b.Name,
						"change", string(crJSON),
						"error", err,
					)
					continue
				}

				idx := len(nodes)
				nodes = append(nodes, &stackedChange{
					Change:       downstackCR,
					Base:         lastDownstackIdx,
					urlFormatter: urlFormatter,
				})
				// Inform previous node about this node.
				if lastDownstackIdx != -1 {
					nodes[lastDownstackIdx].Aboves = append(nodes[lastDownstackIdx].Aboves, idx)
				}
				lastDownstackIdx = idx
			}
		}

		// If this branch's base is known, it'll be in idxByBranch.
		// Otherwise it's trunk (-1) or a merged downstack branch,
		// in which case we'll use the last of those.
		baseIdx := lastDownstackIdx
		if idx, ok := idxByBranch[b.Base]; ok {
			// Tracked base always takes precedence.
			baseIdx = idx
		}

		// If the base is a known node, connect it in both directions.
		if baseIdx != -1 {
			node := nodes[nodeIdx]
			node.Base = baseIdx

			base := nodes[baseIdx]
			base.Aboves = append(base.Aboves, nodeIdx)
		}
	}

	// Third pass:
	// Compute list of branches to sync based on the navCommentSync.
	var branchesToSync []int
	switch navCommentSync {
	case NavCommentSyncBranch:
		// Only submitted branches get commented on.
		for _, branch := range submittedBranches {
			idx, ok := idxByBranch[branch]
			if !ok {
				continue
			}
			branchesToSync = append(branchesToSync, idx)
		}

	case NavCommentSyncDownstack:
		// For each submitted branch,
		// all downstack branches are also commented on.
		updateBranches := make(map[int]struct{})
		for _, branch := range submittedBranches {
			nodeIdx, ok := idxByBranch[branch]
			if !ok {
				continue
			}

			updateBranches[nodeIdx] = struct{}{}

			baseIdx := nodes[nodeIdx].Base
			for baseIdx != -1 {
				// Only add tracked branches
				// that have corresponding infos entries.
				// Merged downstack nodes don't have info
				// and can't be commented on.
				if baseIdx < len(infos) {
					updateBranches[baseIdx] = struct{}{}
				}
				baseIdx = nodes[baseIdx].Base
			}
		}

		branchesToSync = slices.Sorted(maps.Keys(updateBranches))
	}

	type (
		postComment struct {
			Branch string
			Meta   forge.ChangeMetadata

			Change forge.ChangeID
			Body   string
		}
		updateComment struct {
			Branch  string
			Meta    forge.ChangeMetadata
			Change  forge.ChangeID
			Comment forge.ChangeCommentID
			Body    string
		}
	)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex // guards branchTx
		upserted []string
	)
	branchTx := store.BeginBranchTx()
	handlePostComment := func(post *postComment) error {
		commentID, err := remoteRepo.PostChangeComment(ctx, post.Change, post.Body)
		if err != nil {
			return err
		}

		meta := post.Meta
		meta.SetNavigationCommentID(commentID)
		bs, err := remoteRepo.Forge().MarshalChangeMetadata(meta)
		if err != nil {
			return fmt.Errorf("marshal change metadata: %w", err)
		}

		mu.Lock()
		defer mu.Unlock()

		if err := branchTx.Upsert(ctx, state.UpsertRequest{
			Name:           post.Branch,
			ChangeMetadata: bs,
			ChangeForge:    remoteRepo.Forge().ID(),
		}); err != nil {
			return fmt.Errorf("update branch metadata: %w", err)
		}

		upserted = append(upserted, post.Branch)
		return nil
	}

	handleUpdateComment := func(update *updateComment) error {
		err := remoteRepo.UpdateChangeComment(ctx, update.Comment, update.Body)
		if err == nil {
			return nil
		}

		recreatable := errors.Is(err, forge.ErrNotFound) ||
			errors.Is(err, forge.ErrCommentCannotUpdate)
		if !recreatable {
			return err
		}

		log.Info("Recreating navigation comment",
			"change", update.Change.String(),
			"reason", err,
		)

		if err := handlePostComment(&postComment{
			Branch: update.Branch,
			Meta:   update.Meta,
			Change: update.Change,
			Body:   update.Body,
		}); err != nil {
			return fmt.Errorf("post replacement comment: %w", err)
		}

		return nil
	}

	postc := make(chan *postComment)
	updatec := make(chan *updateComment)
	for range min(runtime.GOMAXPROCS(0), len(branchesToSync)) {
		wg.Go(func() {
			postc := postc
			updatec := updatec
			for postc != nil || updatec != nil {
				select {
				case post, ok := <-postc:
					if !ok {
						postc = nil
						continue
					}

					if err := handlePostComment(post); err != nil {
						log.Warn("Error posting comment",
							"change", post.Change.String(),
							"error", err,
						)
						continue
					}

				case update, ok := <-updatec:
					if !ok {
						updatec = nil
						continue
					}

					if err := handleUpdateComment(update); err != nil {
						log.Warn("Error updating comment",
							"change", update.Change.String(),
							"error", err,
						)
						continue
					}
				}
			}
		})
	}

	// Concurrently post and update comments.
	for _, idx := range branchesToSync {
		// If we're only posting on multiple,
		// we'll need to check if the branch is part of a stack
		// that has at least one other branch.
		if navComment == NavCommentOnMultiple {
			if len(nodes[idx].Aboves) == 0 && nodes[idx].Base == -1 {
				continue
			}
		}

		info := infos[idx]
		commentBody := generateStackNavigationComment(nodes, idx, navCommentMarker, remoteRepo.Forge())
		if info.Meta.NavigationCommentID() == nil {
			postc <- &postComment{
				Branch: info.Branch,
				Meta:   info.Meta,
				Change: info.Meta.ChangeID(),
				Body:   commentBody,
			}
		} else {
			updatec <- &updateComment{
				Branch:  info.Branch,
				Meta:    info.Meta,
				Change:  info.Meta.ChangeID(),
				Comment: info.Meta.NavigationCommentID(),
				Body:    commentBody,
			}
		}
	}
	close(postc)
	close(updatec)
	wg.Wait()

	var msg strings.Builder
	msg.WriteString("Post stack navigation comments\n\n")
	for _, name := range upserted {
		fmt.Fprintf(&msg, "- %s\n", name)
	}

	if err := branchTx.Commit(ctx, msg.String()); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	return nil
}

type stackedChange struct {
	Change forge.ChangeID

	Base   int // -1 = no base CR
	Aboves []int

	// urlFormatter, if set, formats the change as a markdown link.
	// Used for forges that don't auto-link change references (e.g., Bitbucket).
	urlFormatter func(forge.ChangeID) string
}

var _ stacknav.Node = (*stackedChange)(nil)

func (s *stackedChange) BaseIdx() int { return s.Base }

func (s *stackedChange) Value() string {
	if s.urlFormatter != nil {
		return s.urlFormatter(s.Change)
	}
	return s.Change.String()
}

const (
	_commentHeader = "This change is part of the following stack:"
	_commentFooter = "<sub>Change managed by [git-spice](https://abhinav.github.io/git-spice/).</sub>"
	_commentMarker = "<!-- gs:navigation comment -->"
)

// Alternate marker for forges that don't support HTML comments.
// Uses Markdown link definition syntax which is invisible when rendered.
const _markdownCommentMarker = "[gs]: # (navigation comment)"

// Regular expressions that must ALL match a comment
// for it to be considered a navigation comment
// when detecting existing comments.
var _navCommentRegexes = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^\Q` + _commentHeader + `\E$`),
	// Match either standard HTML comment or Markdown link definition marker.
	regexp.MustCompile(`(?m)^(\Q` + _commentMarker + `\E|\Q` + _markdownCommentMarker + `\E)$`),
}

func generateStackNavigationComment(
	nodes []*stackedChange,
	current int,
	marker string,
	f forge.Forge,
) string {
	footer := _commentFooter
	commentMarker := _commentMarker
	if fc, ok := f.(forge.WithCommentFormat); ok {
		format := fc.CommentFormat()
		if format.Footer != "" {
			footer = format.Footer
		}
		if format.Marker != "" {
			commentMarker = format.Marker
		}
	}

	var sb strings.Builder
	sb.WriteString(_commentHeader)
	sb.WriteString("\n\n")

	var opts *stacknav.PrintOptions
	if marker != "" {
		opts = &stacknav.PrintOptions{Marker: marker}
	}
	stacknav.Print(&sb, nodes, current, opts)

	sb.WriteString("\n")
	sb.WriteString(footer)

	sb.WriteString("\n")
	sb.WriteString(commentMarker)
	sb.WriteString("\n")
	return sb.String()
}

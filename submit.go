package main

import (
	"context"
	"encoding"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
)

// submitSession is a single session of submitting branches.
// This provides the ability to share state between
// the multiple 'branch submit' invocations made by
// 'stack submit', 'upstack submit', and 'downstack submit'.
type submitSession struct {
	// Branches that have been submitted (created or updated)
	// in this session.
	branches []string

	// Values that are memoized across multiple branch submits.
	Remote     memoizedValue[string]
	RemoteRepo memoizedValue[forge.Repository]
}

func newSubmitSession(
	repo *git.Repository,
	store *state.Store,
	stash secret.Stash,
	view ui.View,
	log *log.Logger,
) *submitSession {
	var s submitSession
	s.Remote.get = func(ctx context.Context) (string, error) {
		return ensureRemote(ctx, repo, store, log, view)
	}

	s.RemoteRepo.get = func(ctx context.Context) (forge.Repository, error) {
		remote, err := s.Remote.Get(ctx)
		if err != nil {
			return nil, err
		}

		return openRemoteRepository(ctx, log, stash, repo, remote)
	}
	return &s
}

// This whole type is a bit of a hack.
// We should have better plumbing and retention of information
// between the submits.
// Maybe newSubmitSession should handle opening remote repo.
type memoizedValue[A any] struct {
	once sync.Once

	val A
	err error
	get func(context.Context) (A, error)
}

func (m *memoizedValue[A]) Get(ctx context.Context) (_ A, err error) {
	m.once.Do(func() { m.val, m.err = m.get(ctx) })
	return m.val, m.err
}

// navigationCommentWhen specifies when a navigation comment should be posted
// (or updated if it already exists).
type navigationCommentWhen int

const (
	// navigationCommentAlways always posts a navigation comment.
	// This is the default.
	navigationCommentAlways navigationCommentWhen = iota

	// navigationCommentNever disables posting navigation comments.
	// If an existing comment is found, it is left as is.
	navigationCommentNever

	// navigationCommentOnMultiple posts a navigation comment
	// only if there are multiple branches in the stack
	// that the current branch is part of.
	navigationCommentOnMultiple
)

var _ encoding.TextUnmarshaler = (*navigationCommentWhen)(nil)

func (f *navigationCommentWhen) UnmarshalText(bs []byte) error {
	switch string(bs) {
	case "true", "1", "yes":
		*f = navigationCommentAlways
	case "false", "0", "no":
		*f = navigationCommentNever
	case "multiple":
		*f = navigationCommentOnMultiple
	default:
		return fmt.Errorf("invalid value %q: expected true, false, or multiple", bs)
	}
	return nil
}

func (f navigationCommentWhen) String() string {
	switch f {
	case navigationCommentAlways:
		return "true"
	case navigationCommentNever:
		return "false"
	case navigationCommentOnMultiple:
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
	store *state.Store,
	svc *spice.Service,
	log *log.Logger,
	navComment navigationCommentWhen,
	session *submitSession,
) error {
	submittedBranches := session.branches
	if len(submittedBranches) == 0 {
		return nil
	}

	remoteRepo, err := session.RemoteRepo.Get(ctx)
	if err != nil {
		return fmt.Errorf("resolve remote repository: %w", err)
	}

	if navComment == navigationCommentNever {
		return nil // nothing to do
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
			Change:         b.Change.ChangeID(),
			Base:           -1,
			MergedBranches: b.MergedBranches,
		})
		infos = append(infos, branchInfo{
			Branch: b.Name,
			Meta:   b.Change,
		})
	}

	// Second pass: connect Aboves.
	for _, b := range trackedBranches {
		nodeIdx, ok := idxByBranch[b.Name]
		if !ok {
			continue
		}

		baseIdx, ok := idxByBranch[b.Base]
		if !ok {
			continue
		}

		node := nodes[nodeIdx]
		node.Base = baseIdx

		base := nodes[baseIdx]
		base.Aboves = append(base.Aboves, nodeIdx)
	}

	type (
		postComment struct {
			Branch string
			Meta   forge.ChangeMetadata

			Change forge.ChangeID
			Body   string
		}
		updateComment struct {
			Change  forge.ChangeID
			Comment forge.ChangeCommentID
			Body    string
		}
	)

	postc := make(chan *postComment)
	updatec := make(chan *updateComment)
	branchTx := store.BeginBranchTx()
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex // guards branchTx
		upserted []string
	)
	for range min(runtime.GOMAXPROCS(0), len(submittedBranches)) {
		wg.Add(1)
		go func() {
			defer wg.Done()

			postc := postc
			updatec := updatec
			for postc != nil || updatec != nil {
				select {
				case post, ok := <-postc:
					if !ok {
						postc = nil
						continue
					}

					commentID, err := remoteRepo.PostChangeComment(ctx, post.Change, post.Body)
					if err != nil {
						log.Warn("Error posting comment",
							"change", post.Change.String(),
							"error", err,
						)
						continue
					}

					meta := post.Meta
					meta.SetNavigationCommentID(commentID)
					bs, err := remoteRepo.Forge().MarshalChangeMetadata(meta)
					if err != nil {
						log.Warn("Error marshaling change metadata",
							"change", post.Change.String(),
							"error", err,
						)
						continue
					}

					mu.Lock()
					if err := branchTx.Upsert(ctx, state.UpsertRequest{
						Name:           post.Branch,
						ChangeMetadata: bs,
						ChangeForge:    remoteRepo.Forge().ID(),
					}); err != nil {
						log.Error("Unable to update branch metadata",
							"branch", post.Branch,
							"error", err,
						)
					} else {
						upserted = append(upserted, post.Branch)
					}
					mu.Unlock()

				case update, ok := <-updatec:
					if !ok {
						updatec = nil
						continue
					}

					err := remoteRepo.UpdateChangeComment(ctx, update.Comment, update.Body)
					if err != nil {
						log.Warn("Error updating comment",
							"change", update.Change.String(),
							"error", err,
						)
						continue
					}
				}
			}
		}()
	}

	// Concurrently post and update comments.
	for _, branch := range submittedBranches {
		idx, ok := idxByBranch[branch]
		if !ok {
			// This should never happen.
			log.Warnf("branch %q not found in tracked branches", branch)
			continue
		}

		// If we're only posting on multiple,
		// we'll need to check if the branch is part of a stack
		// that has at least one other branch.
		if navComment == navigationCommentOnMultiple {
			if len(nodes[idx].Aboves) == 0 && nodes[idx].Base == -1 {
				continue
			}
		}

		info := infos[idx]
		commentBody := generateStackNavigationComment(nodes, idx)
		if info.Meta.NavigationCommentID() == nil {
			postc <- &postComment{
				Branch: branch,
				Meta:   info.Meta,
				Change: info.Meta.ChangeID(),
				Body:   commentBody,
			}
		} else {
			updatec <- &updateComment{
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

	MergedBranches []string
}

const (
	_commentHeader = "This change is part of the following stack:"
	_commentFooter = "<sub>Change managed by [git-spice](https://abhinav.github.io/git-spice/).</sub>"
	_commentMarker = "<!-- gs:navigation comment -->"
)

// Regular expressions that must ALL match a comment
// for it to be considered a navigation comment
// when detecting existing comments.
var _navCommentRegexes = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^\Q` + _commentHeader + `\E$`),
	regexp.MustCompile(`(?m)^\Q` + _commentMarker + `\E$`),
}

func writeMergedChanges(node *stackedChange, sb *strings.Builder, indent int) int {
	for _, mb := range node.MergedBranches {
		sb.WriteString(fmt.Sprintf("%s- %v\n", strings.Repeat(" ", indent), mb))
		indent++
	}
	return indent
}

func generateStackNavigationComment(
	nodes []*stackedChange,
	current int,
) string {
	var sb strings.Builder
	sb.WriteString(_commentHeader)
	sb.WriteString("\n\n")
	write := func(nodeIdx, indent int) {
		node := nodes[nodeIdx]
		for range indent {
			sb.WriteString("    ")
		}
		fmt.Fprintf(&sb, "- %v", node.Change)
		if nodeIdx == current {
			sb.WriteString(" ◀")
		}
		sb.WriteString("\n")
	}

	// The graph is a DAG, so we don't expect cycles.
	// Guard against it anyway.
	visited := make([]bool, len(nodes))
	ok := func(i int) bool {
		if i < 0 || i >= len(nodes) || visited[i] {
			return false
		}
		visited[i] = true
		return true
	}
	isCurrentBasedOnBase := false
	// Write the downstacks, not including the current node.
	// This will change the indent level.
	// The downstacks leading up to the current branch are always linear.
	var indent int
	{
		var downstacks []int
		for base := nodes[current].Base; ok(base); base = nodes[base].Base {
			downstacks = append(downstacks, base)
		}

		if len(downstacks) > 0 {
			base := downstacks[len(downstacks)-1]
			indent = writeMergedChanges(nodes[base], &sb, indent)
		} else {
			isCurrentBasedOnBase = true
		}

		// Reverse order to print from base to current.
		for i := len(downstacks) - 1; i >= 0; i-- {
			write(downstacks[i], indent)
			indent++
		}
	}

	if isCurrentBasedOnBase {
		indent = writeMergedChanges(nodes[current], &sb, indent)
	}

	// For the upstacks, we'll need to traverse the graph
	// and recursively write the upstacks.
	// Indentation will increase for each subtree.
	var visit func(int, int)
	visit = func(nodeIdx, indent int) {
		if !ok(nodeIdx) {
			return
		}

		write(nodeIdx, indent)
		for _, aboveIdx := range nodes[nodeIdx].Aboves {
			visit(aboveIdx, indent+1)
		}
	}

	// Current branch and its upstacks.
	visit(current, indent)
	sb.WriteString("\n")

	sb.WriteString(_commentFooter)
	sb.WriteString("\n")

	sb.WriteString(_commentMarker)
	sb.WriteString("\n")

	return sb.String()
}

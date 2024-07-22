package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

// submitSession is a single session of submitting branches.
// This provides the ability to share state between
// the multiple 'branch submit' invocations made by
// 'stack submit', 'upstack submit', and 'downstack submit'.
//
// The zero value of this type is a valid empty session.
type submitSession struct {
	// Branches that have been submitted (created or updated)
	// in this session.
	branches []string

	// Values that are memoized across multiple branch submits.
	remote     memoizedValue[string]
	remoteRepo memoizedValue[forge.Repository]
}

// This whole type is a bit of a hack.
// We should have better plumbing and retention of information
// between the submits.
// Maybe newSubmitSession should handle opening remote repo.
type memoizedValue[A any] struct {
	once  sync.Once
	done  bool
	value A
}

func (m *memoizedValue[A]) Require() A {
	must.Bef(m.done, "memoized value not set: Require called without Get")
	return m.value
}

func (m *memoizedValue[A]) Get(f func() (A, error)) (_ A, err error) {
	m.once.Do(func() {
		m.value, err = f()
		m.done = true
	})
	return m.value, err
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
func syncStackComments(
	ctx context.Context,
	store *state.Store,
	svc *spice.Service,
	remoteRepo forge.Repository,
	log *log.Logger,
	submittedBranches []string,
) error {
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
			Change: b.Change.ChangeID(),
			Base:   -1,
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
	var (
		wg sync.WaitGroup

		mu      sync.Mutex // guards upserts
		upserts []state.UpsertRequest
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
					meta.SetStackCommentID(commentID)
					bs, err := remoteRepo.Forge().MarshalChangeMetadata(meta)
					if err != nil {
						log.Warn("Error marshaling change metadata",
							"change", post.Change.String(),
							"error", err,
						)
						continue
					}

					mu.Lock()
					upserts = append(upserts, state.UpsertRequest{
						Name:           post.Branch,
						ChangeMetadata: bs,
						ChangeForge:    remoteRepo.Forge().ID(),
					})
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

		info := infos[idx]
		commentBody := generateStackComment(nodes, idx)
		if info.Meta.StackCommentID() == nil {
			postc <- &postComment{
				Branch: branch,
				Meta:   info.Meta,
				Change: info.Meta.ChangeID(),
				Body:   commentBody,
			}
		} else {
			updatec <- &updateComment{
				Change:  info.Meta.ChangeID(),
				Comment: info.Meta.StackCommentID(),
				Body:    commentBody,
			}
		}
	}
	close(postc)
	close(updatec)
	wg.Wait()

	if len(upserts) == 0 {
		return nil
	}

	var msg strings.Builder
	msg.WriteString("Post stack comments\n\n")
	for _, upsert := range upserts {
		fmt.Fprintf(&msg, "- %s\n", upsert.Name)
	}

	err = store.UpdateBranch(ctx, &state.UpdateRequest{
		Upserts: upserts,
		Message: msg.String(),
	})
	if err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	return nil
}

type stackedChange struct {
	Change forge.ChangeID

	Base   int // -1 = no base CR
	Aboves []int
}

const (
	_commentHeader = "This change is part of the following stack:\n\n"
	_commentFooter = "\n<sub>Change managed by [git-spice](https://abhinav.github.io/git-spice/).</sub>\n"
)

func generateStackComment(
	nodes []*stackedChange,
	current int,
) string {
	var sb strings.Builder
	sb.WriteString(_commentHeader)
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

	// Write the downstacks, not including the current node.
	// This will change the indent level.
	// The downstacks leading up to the current branch are always linear.
	var indent int
	{
		var downstacks []int
		for base := nodes[current].Base; ok(base); base = nodes[base].Base {
			downstacks = append(downstacks, base)
		}

		// Reverse order to print from base to current.
		for i := len(downstacks) - 1; i >= 0; i-- {
			write(downstacks[i], indent)
			indent++
		}
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
	sb.WriteString(_commentFooter)
	return sb.String()
}

package merge

import (
	"context"
	"errors"
	"fmt"
	"slices"
	stdsync "sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"pgregory.net/rapid"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/handler/sync"
	"go.abhg.dev/gs/internal/spice"
)

func TestMergePlanExecutor_Execute_validForests(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		count := rapid.IntRange(0, 20).Draw(t, "count")
		names := rapid.SliceOfNDistinct(
			rapid.StringMatching(`[a-z]{1,8}`).Filter(func(s string) bool { return s != "main" }),
			count, count, func(s string) string { return s },
		).Draw(t, "branches")
		allSucceed := rapid.Bool().Draw(t, "allSucceed")
		model := &mergeForestModel{
			changes:  make(map[string]*mergeForestChange),
			branches: make(map[string]*mergeForestBranch),
			stacks:   make(map[int][]forge.ChangeID),
			trunk:    "trunk-0",
			merged:   make(map[string]struct{}),
			failed:   make(map[string]struct{}),
			failure:  errors.New("injected merge failure"),
		}
		items := make([]*mergeItem, count)
		aboves := make([][]int, count)
		roots := make([]string, count)
		failureRoots := make(map[string]struct{})
		for idx, name := range names {
			// Choosing only earlier branches as bases constructs a forest directly,
			// including multiple roots and forks, without filtering.
			base := rapid.IntRange(-1, idx-1).Draw(t, "base")
			baseBranch, baseID := "main", ""
			roots[idx] = name
			if base >= 0 {
				baseBranch = names[base]
				baseID = items[base].changeID.String()
				roots[idx] = roots[base]
				aboves[base] = append(aboves[base], idx)
			}
			id := fakeChangeID(fmt.Sprintf("pr-%d", idx+1))
			items[idx] = testPlanEntry(name, baseBranch, id)
			items[idx].headHash = git.Hash("head-" + name)
			fails := !allSucceed && rapid.Bool().Draw(t, "individualFailure")
			model.changes[id.String()] = &mergeForestChange{
				branch: name, dependency: baseID, base: baseID,
				head: items[idx].headHash, fails: fails,
			}
			baseHead := model.trunk
			if base >= 0 {
				baseHead = items[base].headHash
			}
			model.branches[name] = &mergeForestBranch{
				change: id.String(),
				base:   baseID, baseHead: baseHead, head: items[idx].headHash,
				dissolveOnSubmit: rapid.Bool().Draw(t, "dissolveOnSubmit"),
			}
			if fails {
				failureRoots[roots[idx]] = struct{}{}
			}
		}

		// Native plans may cover any disjoint linear paths,
		// not necessarily entire trees or the longest paths.
		// Omitted changes merge normally.
		var plans []forge.MergeRangePlan
		claimed := make(map[int]struct{})
		for idx := range items {
			if _, ok := claimed[idx]; ok || !rapid.Bool().Draw(t, "native") {
				continue
			}
			var changes []forge.ChangeID
			for at := idx; ; {
				claimed[at] = struct{}{}
				changes = append(changes, items[at].changeID)
				if len(aboves[at]) == 0 || !rapid.Bool().Draw(t, "extend") {
					break
				}
				at = rapid.SampledFrom(aboves[at]).Draw(t, "above")
			}
			fails := !allSucceed && rapid.Bool().Draw(t, "nativeFailure")
			var failure error
			if fails {
				failure = model.failure
				failureRoots[roots[idx]] = struct{}{}
			}
			stack := len(plans) + 1
			model.stacks[stack] = changes
			plans = append(plans, &testMergeRangePlan{
				changes: changes,
				merge: func(_ context.Context, req forge.MergeRangeRequest) (forge.MergeOperation, error) {
					return nil, model.mergeRange(changes, req, failure)
				},
			})
		}
		items = rapid.Permutation(items).Draw(t, "planOrder")
		plans = rapid.Permutation(plans).Draw(t, "rangeOrder")

		// Preparation drives the model through the real workspace code.
		// Remote completion is immediate; timing and workspace interleavings
		// retain deterministic coverage elsewhere.
		ctrl := gomock.NewController(t)
		service := NewMockService(ctrl)
		service.EXPECT().
			VerifyRestacked(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, branch string) error {
				return model.verifyRestacked(branch)
			}).
			AnyTimes()
		gitRepo := NewMockGitRepository(ctrl)
		gitRepo.EXPECT().
			PeelToCommit(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, branch string) (git.Hash, error) {
				model.mu.Lock()
				defer model.mu.Unlock()
				return model.branches[branch].head, nil
			}).
			AnyTimes()
		restackHandler := NewMockRestackHandler(ctrl)
		restackHandler.EXPECT().RestackBranch(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, req *restack.BranchRequest) error {
				model.restack(req.Branch)
				return nil
			}).AnyTimes()
		submitHandler := NewMockSubmitHandler(ctrl)
		submitHandler.EXPECT().Submit(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, req *submit.Request) error {
				model.submit(req.Branch)
				return nil
			}).AnyTimes()
		progress := new(recordingMergeProgress)
		executor := &mergePlanExecutor{
			RemoteRepository: model,
			Repository:       gitRepo,
			Service:          service,
			Restack:          restackHandler,
			Submit:           submitHandler,
			Sync: syncHandlerFunc(func(context.Context, *sync.TrunkOptions) error {
				model.syncTrunk()
				return nil
			}),
			Progress:         progress,
			MergeRequester:   &forgeMergeRequester{Repository: model},
			ReadinessChecker: &forgeReadinessChecker{Repository: model},
			MergeRangePlans:  plans,
			Trunk:            "main",
			ReadyTimeout:     0,
			MergeTimeout:     time.Second,
			Method:           forge.MergeMethodDefault,
		}
		err := executor.Execute(t.Context(), items)
		t.Logf("effects: %v", model.trace)
		require.Empty(t, model.violations)
		if len(model.failed) == 0 {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, model.failure)
		}

		// Check effects, not an expected schedule.
		// Successful changes must retain their merged outcome
		// even if a later fallback member fails.
		terminal := make(map[string][]mergeProgressEventKind)
		for _, event := range progress.events {
			switch event.Kind {
			case mergeProgressMerged, mergeProgressFailed, mergeProgressSkipped:
				key := event.Item.changeID.String()
				terminal[key] = append(terminal[key], event.Kind)
			}
		}
		t.Logf("terminal outcomes: %v; execute error: %v", terminal, err)
		require.Len(t, terminal, len(items))
		for _, item := range items {
			id := item.changeID.String()
			require.Len(t, terminal[id], 1, "%s: terminal outcome", item.branch)
			_, merged := model.merged[id]
			assert.Equal(t, merged, terminal[id][0] == mergeProgressMerged, "%s: merged effect", item.branch)
		}
		// A tree with no possible injected failure must finish
		// even when another tree fails.
		// This avoids predicting skips within failed trees.
		for idx, root := range roots {
			if _, ok := failureRoots[root]; !ok {
				_, merged := model.merged[fmt.Sprintf("pr-%d", idx+1)]
				assert.True(t, merged, "%s: unaffected tree %s must finish", names[idx], root)
			}
		}
	})
}

// mergeForestModel keeps local branches separate from published PRs.
// The real workspace drives synchronization, restacking and submission;
// this model supplies their effects without simulating Git commits or the
// native-stack reconciliation algorithm. Remote merges complete immediately.
type mergeForestModel struct {
	forge.Repository

	changes  map[string]*mergeForestChange // keyed by change ID
	branches map[string]*mergeForestBranch // keyed by branch name
	stacks   map[int][]forge.ChangeID      // current native membership, bottom-to-top
	trunk    git.Hash                      // locally synchronized trunk
	commit   int                           // supplies distinct heads after restacking
	failure  error

	// mu guards all modeled state while queue items run concurrently.
	mu         stdsync.Mutex
	merged     map[string]struct{}
	failed     map[string]struct{}
	trace      []string
	violations []string
}

type mergeForestChange struct {
	branch string

	// dependency preserves the user's selected forest as bases evolve.
	// A successful merge must still include or follow that original dependency.
	dependency string

	base  string   // published base change ID; empty for trunk
	head  git.Hash // published commit; updated only by submission
	fails bool     // ordinary merge failure, including native fallback
}

type mergeForestBranch struct {
	change   string // published change ID
	base     string // current local base change ID; empty for trunk
	head     git.Hash
	baseHead git.Hash // base commit incorporated by the last restack

	// dissolveOnSubmit models a submission that leaves its native resource
	// dissolved instead of restoring membership. Other submissions preserve it.
	dissolveOnSubmit bool
}

func (r *mergeForestModel) verifyRestacked(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	branch := r.branches[name]
	if branch.baseHead != r.baseHead(branch.base) {
		return &spice.BranchNeedsRestackError{Base: branch.base}
	}
	return nil
}

func (r *mergeForestModel) restack(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	branch := r.branches[name]
	branch.baseHead = r.baseHead(branch.base)
	r.commit++
	branch.head = git.Hash(fmt.Sprintf("restacked-%s-%d", name, r.commit))
	r.trace = append(r.trace, fmt.Sprintf("restack %s onto %s", name, branch.baseHead))
}

func (r *mergeForestModel) submit(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	branch := r.branches[name]
	change := r.changes[branch.change]
	change.head, change.base = branch.head, branch.base
	r.trace = append(r.trace, fmt.Sprintf("submit %s at %s", name, branch.head))
	if branch.dissolveOnSubmit {
		if stack := r.stackFor(branch.change); stack != 0 {
			delete(r.stacks, stack)
			r.trace = append(r.trace, fmt.Sprintf("dissolve stack %d", stack))
		}
	}
}

func (r *mergeForestModel) syncTrunk() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trunk = git.Hash(fmt.Sprintf("trunk-%d", len(r.merged)))
	for _, branch := range r.branches {
		// Removing merged bases changes a branch's target, but not its commits.
		// A later VerifyRestacked must therefore require preparation before
		// publishing its new head. Native neighbors also become stale when
		// restacking changes the head they previously incorporated.
		for branch.base != "" {
			if _, merged := r.merged[branch.base]; !merged {
				break
			}
			branch.base = r.changes[branch.base].dependency
		}
	}
	r.trace = append(r.trace, fmt.Sprintf("sync %s", r.trunk))
}

// baseHead and stackFor require ownership of mu.
func (r *mergeForestModel) baseHead(base string) git.Hash {
	if base == "" {
		return r.trunk
	}
	return r.branches[r.changes[base].branch].head
}

func (r *mergeForestModel) stackFor(id string) int {
	for stack, members := range r.stacks {
		for _, member := range members {
			if member.String() == id {
				return stack
			}
		}
	}
	return 0
}

func (r *mergeForestModel) mergeRange(
	changes []forge.ChangeID, req forge.MergeRangeRequest, failure error,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// The plan was captured before preparation. Submission may have dissolved
	// its resource since then, so eligibility comes from current membership,
	// not from a preselected outcome attached to that plan.
	// Membership loss uses the contract's unsupported case to exercise handler
	// fallback, not any specific provider's error classification.
	stack := r.stackFor(changes[0].String())
	if stack == 0 || !slices.EqualFunc(r.stacks[stack], changes, func(a, b forge.ChangeID) bool {
		return a.String() == b.String()
	}) {
		r.trace = append(r.trace, fmt.Sprintf("no longer native: %v", changes))
		return forge.ErrUnsupported
	}
	heads := make(map[string]git.Hash, len(req.Heads))
	for _, head := range req.Heads {
		heads[head.Change.String()] = head.HeadHash
	}
	for i := 1; i < len(changes); i++ {
		below := r.changes[changes[i-1].String()]
		above := r.changes[changes[i].String()]
		branch := r.branches[above.branch]
		if above.base != changes[i-1].String() || branch.baseHead != below.head {
			err := fmt.Errorf("native range %v is not fully restacked and submitted", changes)
			r.violations = append(r.violations, err.Error())
			return err
		}
	}
	return r.merge(changes, heads, failure)
}

// merge requires ownership of mu. Validate the whole request before applying
// any effect: both ordinary and native requests either succeed or fail in full.
// A failed request must not be retried through ordinary fallback.
func (r *mergeForestModel) merge(changes []forge.ChangeID, heads map[string]git.Hash, failure error) error {
	outcome := "merge"
	if failure != nil {
		outcome = "fail"
	}
	r.trace = append(r.trace, fmt.Sprintf("%s %v", outcome, changes))
	request := make(map[string]struct{}, len(changes))
	for _, id := range changes {
		request[id.String()] = struct{}{}
	}
	for _, id := range changes {
		key := id.String()
		change, selected := r.changes[key]
		_, merged := r.merged[key]
		_, failed := r.failed[key]
		if !selected {
			err := fmt.Errorf("unselected merge of %s", key)
			r.violations = append(r.violations, err.Error())
			return err
		}
		_, baseMerged := r.merged[change.dependency]
		_, baseIncluded := request[change.dependency]
		if merged || failed || (change.dependency != "" && !baseMerged && !baseIncluded) {
			err := fmt.Errorf("invalid merge of %s: selected=%v merged=%v failed=%v base=%s merged=%v included=%v",
				key, selected, merged, failed, change.dependency, baseMerged, baseIncluded)
			r.violations = append(r.violations, err.Error())
			return err
		}
		if heads[key] != change.head {
			err := fmt.Errorf("merge of %s expects %s, published head is %s", key, heads[key], change.head)
			r.violations = append(r.violations, err.Error())
			return err
		}
		// Merging into an old base branch is not merging into trunk.
		// Submission must retarget it, unless this request merges that base
		// atomically with the change.
		if _, included := request[change.base]; change.base != "" && !included {
			err := fmt.Errorf("merge of %s still targets %s", key, change.base)
			r.violations = append(r.violations, err.Error())
			return err
		}
	}
	if failure != nil {
		for key := range request {
			r.failed[key] = struct{}{}
		}
		return failure
	}
	for key := range request {
		r.merged[key] = struct{}{}
	}
	return nil
}

func (r *mergeForestModel) MergeChange(_ context.Context, id forge.ChangeID, opts forge.MergeChangeOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var failure error
	if r.changes[id.String()].fails {
		failure = r.failure
	}
	return r.merge([]forge.ChangeID{id}, map[string]git.Hash{id.String(): opts.HeadHash}, failure)
}

func (r *mergeForestModel) ChangeMergeability(context.Context, forge.ChangeID) (forge.ChangeMergeability, error) {
	return forge.ChangeMergeability{State: forge.ChangeMergeabilityReady}, nil
}

func (r *mergeForestModel) ChangeStatuses(_ context.Context, ids []forge.ChangeID) ([]forge.ChangeStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	statuses := make([]forge.ChangeStatus, len(ids))
	for idx, id := range ids {
		state := forge.ChangeOpen
		if _, ok := r.merged[id.String()]; ok {
			state = forge.ChangeMerged
		}
		statuses[idx] = forge.ChangeStatus{State: state, HeadHash: r.changes[id.String()].head}
	}
	return statuses, nil
}

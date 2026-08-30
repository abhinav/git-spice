package shamhub

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/graph"
)

type planMergeRangesRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	Changes []stackChange `json:"changes"`
}

type planMergeRangesResponse struct {
	Ranges [][]int `json:"ranges"`
}

var _ = shamhubRESTHandler(
	"POST /{owner}/{repo}/stack/merge-ranges/plan",
	(*ShamHub).handlePlanMergeRanges,
)

func (sh *ShamHub) handlePlanMergeRanges(
	_ context.Context,
	req *planMergeRangesRequest,
) (*planMergeRangesResponse, error) {
	requestedBaseByChange := make(map[int]int, len(req.Changes))
	for _, change := range req.Changes {
		requestedBaseByChange[change.Number] = change.Base
	}

	sh.mu.RLock()
	storedBaseByChange := maps.Clone(
		sh.stackBases[repoID{Owner: req.Owner, Name: req.Repo}],
	)
	sh.mu.RUnlock()

	// Keep only relationships represented by both the selected forest and
	// ShamHub's current native stacks. A selected base must be the stored base;
	// a stored base outside the selection remains the range's external base.
	eligibleBaseByChange := make(map[int]int, len(req.Changes))
	for change, requestedBase := range requestedBaseByChange {
		storedBase, stacked := storedBaseByChange[change]
		if !stacked {
			continue
		}
		_, storedBaseSelected := requestedBaseByChange[storedBase]
		if requestedBase != 0 && requestedBase != storedBase ||
			requestedBase == 0 && storedBaseSelected {
			continue
		}
		eligibleBaseByChange[change] = storedBase
	}

	ordered, err := graph.Toposort(
		slices.Sorted(maps.Keys(eligibleBaseByChange)),
		func(change int) (int, bool) {
			base := eligibleBaseByChange[change]
			_, selected := eligibleBaseByChange[base]
			return base, selected
		},
	)
	if err != nil {
		return nil, fmt.Errorf("order native stack changes: %w", err)
	}

	aboves := make(map[int][]int, len(ordered))
	for _, change := range ordered {
		base := eligibleBaseByChange[change]
		if _, selected := eligibleBaseByChange[base]; selected {
			aboves[base] = append(aboves[base], change)
		}
	}

	// A fork ends the shared range. Each divergent branch begins another
	// disjoint range, allowing the handler to preserve ordinary scheduling
	// dependencies without assigning one change to multiple atomic operations.
	assigned := make(map[int]struct{}, len(ordered))
	var ranges [][]int
	for _, bottom := range ordered {
		if _, ok := assigned[bottom]; ok {
			continue
		}

		var mergeRange []int
		for current := bottom; ; {
			mergeRange = append(mergeRange, current)
			assigned[current] = struct{}{}
			if len(aboves[current]) != 1 {
				break
			}
			current = aboves[current][0]
		}
		ranges = append(ranges, mergeRange)
	}
	return &planMergeRangesResponse{Ranges: ranges}, nil
}

type mergeRangeChange struct {
	Number   int    `json:"number"`
	Base     string `json:"base"`
	Head     string `json:"head"`
	HeadHash string `json:"headHash"`
}

type mergeRangeRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	Changes     []mergeRangeChange `json:"changes"`
	MergeMethod string             `json:"mergeMethod,omitempty"`
}

type mergeRangeResponse struct{}

var _ = shamhubRESTHandler(
	"POST /{owner}/{repo}/change/merge-range",
	(*ShamHub).handleMergeRange,
)

func (sh *ShamHub) handleMergeRange(
	ctx context.Context,
	req *mergeRangeRequest,
) (*mergeRangeResponse, error) {
	method := MergeMethod(req.MergeMethod)
	if method == "" {
		sh.mu.RLock()
		method = sh.defaultMergeMethod
		sh.mu.RUnlock()
	} else if _, err := parseMergeMethod(string(method)); err != nil {
		return nil, badRequestErrorf("%s", err)
	}

	if err := sh.mergeRange(ctx, req.Owner, req.Repo, req.Changes, method); err != nil {
		return nil, err
	}
	return &mergeRangeResponse{}, nil
}

// preparedMergeRange holds validated server state used to construct and
// publish one atomic range merge.
type preparedMergeRange struct {
	// rootBaseHash is the target branch value used by the publishing CAS.
	rootBaseHash string

	// changes retains the validated server records in bottom-to-top order.
	changes []preparedMergeRangeChange
}

// preparedMergeRangeChange pairs a validated change snapshot with the mutable
// entry updated only after atomic publication succeeds.
type preparedMergeRangeChange struct {
	// index is the snapshot's position in ShamHub.changes.
	index int

	// change is the validated snapshot used to construct the merge result.
	change shamChange

	// headHash is the resolved head value validated against the request.
	headHash string
}

// mergeRange validates and builds the result before one compare-and-swap
// publishes it. Commit construction may leave unreachable objects on failure,
// but the root ref and in-memory change states remain unchanged. Holding mu
// across the ref update and state transition makes those observable effects
// one operation to ShamHub clients.
func (sh *ShamHub) mergeRange(
	ctx context.Context,
	owner string,
	repo string,
	changes []mergeRangeChange,
	method MergeMethod,
) error {
	if owner == "" || repo == "" {
		return errors.New("owner and repo are required")
	}
	if len(changes) == 0 {
		return errors.New("changes must not be empty")
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	prepared, err := sh.prepareMergeRange(ctx, owner, repo, changes)
	if err != nil {
		return err
	}

	commit, err := sh.buildMergeRangeCommit(ctx, owner, repo, method, prepared)
	if err != nil {
		return err
	}

	rootRef := "refs/heads/" + prepared.changes[0].change.Base.Name
	if err := sh.gitCmd(
		ctx,
		owner,
		repo,
		"update-ref",
		rootRef,
		commit,
		prepared.rootBaseHash,
	).Run(); err != nil {
		return fmt.Errorf("update root ref: %w", err)
	}

	for _, change := range prepared.changes {
		sh.changes[change.index].State = shamChangeMerged
		sh.changes[change.index].HeadHash = change.headHash
	}
	return nil
}

func (sh *ShamHub) prepareMergeRange(
	ctx context.Context,
	owner string,
	repo string,
	requested []mergeRangeChange,
) (preparedMergeRange, error) {
	byNumber := make(map[int]int, len(requested))
	for i, change := range sh.changes {
		if change.Base.Owner == owner && change.Base.Repo == repo {
			byNumber[change.Number] = i
		}
	}

	prepared := preparedMergeRange{
		changes: make([]preparedMergeRangeChange, len(requested)),
	}
	for i, expected := range requested {
		changeIndex, ok := byNumber[expected.Number]
		if !ok {
			return preparedMergeRange{}, fmt.Errorf(
				"change %d (%s/%s) not found", expected.Number, owner, repo,
			)
		}
		change := sh.changes[changeIndex]
		if change.State != shamChangeOpen {
			return preparedMergeRange{}, fmt.Errorf(
				"change %d is not open", expected.Number,
			)
		}
		if change.Draft {
			return preparedMergeRange{}, fmt.Errorf(
				"change %d is a draft", expected.Number,
			)
		}
		if change.Base.Name != expected.Base {
			return preparedMergeRange{}, fmt.Errorf(
				"change %d base branch is %q, expected %q",
				expected.Number, change.Base.Name, expected.Base,
			)
		}
		if change.Head.Name != expected.Head {
			return preparedMergeRange{}, fmt.Errorf(
				"change %d head branch is %q, expected %q",
				expected.Number, change.Head.Name, expected.Head,
			)
		}
		if expected.HeadHash == "" {
			return preparedMergeRange{}, fmt.Errorf(
				"change %d head hash is required", expected.Number,
			)
		}
		if i > 0 && expected.Base != requested[i-1].Head {
			return preparedMergeRange{}, fmt.Errorf(
				"change %d base branch is %q, expected prior head %q",
				expected.Number, expected.Base, requested[i-1].Head,
			)
		}

		baseHash, err := sh.gitCmd(
			ctx,
			owner,
			repo,
			"rev-parse",
			"refs/heads/"+change.Base.Name+"^{commit}",
		).OutputChomp()
		if err != nil {
			return preparedMergeRange{}, fmt.Errorf(
				"resolve change %d base: %w", expected.Number, err,
			)
		}
		headHash, err := sh.gitCmd(
			ctx,
			change.Head.Owner,
			change.Head.Repo,
			"rev-parse",
			"refs/heads/"+change.Head.Name+"^{commit}",
		).OutputChomp()
		if err != nil {
			return preparedMergeRange{}, fmt.Errorf(
				"resolve change %d head: %w", expected.Number, err,
			)
		}
		if headHash != expected.HeadHash {
			return preparedMergeRange{}, fmt.Errorf(
				"change %d head hash mismatch: expected %q, got %q",
				expected.Number, expected.HeadHash, headHash,
			)
		}

		// Commit construction runs in the receiving repository.
		// After validating a fork head in its source repository,
		// import its objects into the receiving repository.
		if change.Head.Owner != owner || change.Head.Repo != repo {
			if err := sh.gitCmd(
				ctx,
				owner,
				repo,
				"fetch",
				"--no-write-fetch-head",
				sh.repoDir(change.Head.Owner, change.Head.Repo),
				change.Head.Name,
			).Run(); err != nil {
				return preparedMergeRange{}, fmt.Errorf(
					"fetch change %d head objects: %w", expected.Number, err,
				)
			}
		}

		// Branch-name alignment is insufficient for an atomic range merge.
		// Each base ref must resolve to the previous validated head commit.
		if i > 0 && baseHash != prepared.changes[i-1].headHash {
			return preparedMergeRange{}, fmt.Errorf(
				"change %d base hash is %q, expected prior head %q",
				expected.Number, baseHash, prepared.changes[i-1].headHash,
			)
		}
		if err := sh.gitCmd(
			ctx,
			owner,
			repo,
			"merge-base",
			"--is-ancestor",
			baseHash,
			headHash,
		).Run(); err != nil {
			return preparedMergeRange{}, fmt.Errorf(
				"change %d head is not based on %q: %w",
				expected.Number, expected.Base, err,
			)
		}

		if i == 0 {
			prepared.rootBaseHash = baseHash
		}
		prepared.changes[i] = preparedMergeRangeChange{
			index:    changeIndex,
			change:   change,
			headHash: headHash,
		}
	}
	return prepared, nil
}

func (sh *ShamHub) buildMergeRangeCommit(
	ctx context.Context,
	owner string,
	repo string,
	method MergeMethod,
	prepared preparedMergeRange,
) (string, error) {
	switch method {
	case MergeMethodMerge:
		top := prepared.changes[len(prepared.changes)-1]
		tree, err := sh.gitCmd(
			ctx,
			owner,
			repo,
			"merge-tree",
			"--write-tree",
			prepared.rootBaseHash,
			top.headHash,
		).OutputChomp()
		if err != nil {
			return "", fmt.Errorf("merge range trees: %w", err)
		}
		message := fmt.Sprintf(
			"Merge changes #%d through #%d",
			prepared.changes[0].change.Number,
			top.change.Number,
		)
		return sh.commitRangeTree(
			ctx,
			owner,
			repo,
			tree,
			[]string{prepared.rootBaseHash, top.headHash},
			message,
			top.headHash,
		)

	case MergeMethodSquash:
		// Every stacked head tree contains the changes below it. Re-parenting
		// those trees in range order produces one squashed commit per change
		// without reconstructing or replaying individual patches.
		parent := prepared.rootBaseHash
		for _, change := range prepared.changes {
			tree, err := sh.gitCmd(
				ctx,
				owner,
				repo,
				"rev-parse",
				change.headHash+"^{tree}",
			).OutputChomp()
			if err != nil {
				return "", fmt.Errorf(
					"resolve change %d tree: %w", change.change.Number, err,
				)
			}
			message := fmt.Sprintf(
				"%s (#%d)\n\n%s",
				change.change.Subject,
				change.change.Number,
				change.change.Body,
			)
			parent, err = sh.commitRangeTree(
				ctx,
				owner,
				repo,
				tree,
				[]string{parent},
				message,
				change.headHash,
			)
			if err != nil {
				return "", err
			}
		}
		return parent, nil

	default:
		return "", fmt.Errorf("unsupported merge method %q", method)
	}
}

func (sh *ShamHub) commitRangeTree(
	ctx context.Context,
	owner string,
	repo string,
	tree string,
	parents []string,
	message string,
	timeSource string,
) (string, error) {
	commitTimeText, err := sh.gitCmd(
		ctx,
		owner,
		repo,
		"log",
		"-1",
		"--format=%cI",
		timeSource,
	).OutputChomp()
	if err != nil {
		return "", fmt.Errorf("read commit time: %w", err)
	}
	commitTime, err := time.Parse(time.RFC3339, commitTimeText)
	if err != nil {
		return "", fmt.Errorf("parse commit time: %w", err)
	}

	args := []string{"commit-tree"}
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	args = append(args, "-m", message, tree)
	commit, err := sh.gitCmd(ctx, owner, repo, args...).
		AppendEnv(
			"GIT_COMMITTER_NAME=ShamHub",
			"GIT_COMMITTER_EMAIL=shamhub@example.com",
			"GIT_AUTHOR_NAME=ShamHub",
			"GIT_AUTHOR_EMAIL=shamhub@example.com",
			"GIT_COMMITTER_DATE="+commitTime.Format(time.RFC3339),
			"GIT_AUTHOR_DATE="+commitTime.Format(time.RFC3339),
		).
		OutputChomp()
	if err != nil {
		return "", fmt.Errorf("create merge commit: %w", err)
	}
	return commit, nil
}

// PlanMergeRanges loads ShamHub's stored native-stack relationships and
// returns the disjoint linear ranges it can merge atomically.
func (r *stackRepository) PlanMergeRanges(
	ctx context.Context,
	changes []forge.StackChange,
) ([]forge.MergeRangePlan, error) {
	requestedChanges := make(map[ChangeID]struct{}, len(changes))
	for _, change := range changes {
		requestedChanges[change.Change.(ChangeID)] = struct{}{}
	}

	req := planMergeRangesRequest{
		Changes: make([]stackChange, len(changes)),
	}
	for i, change := range changes {
		req.Changes[i].Number = int(change.Change.(ChangeID))
		base, ok := change.BaseChange.(ChangeID)
		if !ok {
			continue
		}
		if _, selected := requestedChanges[base]; selected {
			req.Changes[i].Base = int(base)
		}
	}

	var res planMergeRangesResponse
	if err := r.client.Post(
		ctx,
		r.apiURL.JoinPath(r.owner, r.repo, "stack", "merge-ranges", "plan").String(),
		req,
		&res,
	); err != nil {
		return nil, fmt.Errorf("plan merge ranges: %w", err)
	}

	plans := make([]forge.MergeRangePlan, len(res.Ranges))
	for i, numbers := range res.Ranges {
		planned := make([]forge.ChangeID, len(numbers))
		for j, number := range numbers {
			planned[j] = ChangeID(number)
		}
		plans[i] = &shamHubMergeRangePlan{
			repository: r,
			changes:    planned,
		}
	}
	return plans, nil
}

type shamHubMergeRangePlan struct {
	repository *stackRepository
	changes    []forge.ChangeID
}

func (p *shamHubMergeRangePlan) Changes() []forge.ChangeID {
	return p.changes
}

// Merge asks ShamHub to atomically merge the planned aligned range.
func (p *shamHubMergeRangePlan) Merge(
	ctx context.Context,
	request forge.MergeRangeRequest,
) (forge.MergeOperation, error) {
	if len(request.Changes) != len(p.changes) {
		return nil, fmt.Errorf(
			"merge range request has %d changes, planned %d",
			len(request.Changes),
			len(p.changes),
		)
	}
	for i, change := range request.Changes {
		if change.Change.String() != p.changes[i].String() {
			return nil, fmt.Errorf(
				"merge range request change %d is %v, planned %v",
				i,
				change.Change,
				p.changes[i],
			)
		}
	}

	req := mergeRangeRequest{
		Changes: make([]mergeRangeChange, len(request.Changes)),
	}
	for i, change := range request.Changes {
		req.Changes[i] = mergeRangeChange{
			Number:   int(change.Change.(ChangeID)),
			Base:     change.Base,
			Head:     change.Head,
			HeadHash: change.HeadHash.String(),
		}
	}
	switch request.Method {
	case forge.MergeMethodMerge, forge.MergeMethodSquash:
		req.MergeMethod = request.Method.String()
	case forge.MergeMethodDefault:
	default:
		p.repository.log.Warn(
			"Unsupported merge method; using forge default",
			"method", request.Method,
		)
	}

	var res mergeRangeResponse
	if err := p.repository.client.Post(
		ctx,
		p.repository.apiURL.JoinPath(
			p.repository.owner,
			p.repository.repo,
			"change",
			"merge-range",
		).String(),
		req,
		&res,
	); err != nil {
		return nil, fmt.Errorf("merge range: %w", err)
	}
	return nil, nil
}

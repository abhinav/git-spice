package merge

import (
	"context"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog"
)

// mergeRequester requests the forge-side merge after readiness checks pass.
//
// A successful request does not mean the change has merged.
// The merge executor must still wait for the forge to report the merged state.
type mergeRequester interface {
	RequestMerge(context.Context, *mergeItem) error
}

// forgeMergeRequester requests merges through the forge API.
type forgeMergeRequester struct {
	Repository forge.Repository // required
	Method     forge.MergeMethod
}

func (r *forgeMergeRequester) RequestMerge(ctx context.Context, item *mergeItem) error {
	return r.Repository.MergeChange(ctx, item.changeID, forge.MergeChangeOptions{
		Method:   r.Method,
		HeadHash: item.headHash,
	})
}

// commandMergeRequester requests merges through a user-configured command.
//
// The command is only the merge request step.
// The caller remains responsible for waiting until the forge reports the
// change as merged.
type commandMergeRequester struct {
	Runner *commandRunner // required
	Script string         // required
}

func (r *commandMergeRequester) RequestMerge(ctx context.Context, item *mergeItem) error {
	result, err := r.Runner.Run(ctx, r.Script, item)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("command exited with status %d", result.ExitCode)
	}
	return nil
}

// readinessChecker reports whether a merge queue item can enter
// the merge request phase.
type readinessChecker interface {
	CheckMergeItemReady(context.Context, *mergeItem) (forge.ChangeMergeability, error)
}

type forgeReadinessChecker struct {
	Repository forge.Repository // required
}

func (r *forgeReadinessChecker) CheckMergeItemReady(ctx context.Context, item *mergeItem) (forge.ChangeMergeability, error) {
	return r.Repository.ChangeMergeability(ctx, item.changeID)
}

type commandReadinessChecker struct {
	Runner *commandRunner // required
	Script string         // required
}

func (r *commandReadinessChecker) CheckMergeItemReady(ctx context.Context, item *mergeItem) (forge.ChangeMergeability, error) {
	result, err := r.Runner.Run(ctx, r.Script, item)
	if err != nil {
		return forge.ChangeMergeability{},
			fmt.Errorf("run readiness command: %w", err)
	}

	switch result.ExitCode {
	case 0:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityReady,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil
	case 1:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityWaiting,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil
	case 2:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil
	default:
		return forge.ChangeMergeability{}, fmt.Errorf(
			"readiness command exited with status %d",
			result.ExitCode,
		)
	}
}

// commandRunner runs a configured merge workflow command
// with shared git-spice and forge-specific environment variables.
type commandRunner struct {
	// Log receives command stdout and stderr.
	Log *silog.Logger // required

	// Repository supplies provider-specific environment variables.
	Repository forge.Repository // required

	// ForgeID identifies the active forge in GIT_SPICE_FORGE_ID.
	ForgeID string // required

	// Trunk is the repository trunk branch name.
	Trunk string // required

	// Runner executes Script with the environment built for the merge item.
	Runner ScriptRunner // required
}

func (r *commandRunner) Run(ctx context.Context, script string, item *mergeItem) (*scriptrun.RunResult, error) {
	env, err := r.environment(ctx, item)
	if err != nil {
		return nil, fmt.Errorf("build environment: %w", err)
	}

	output, flushOutput := silog.Writer(
		r.Log.WithPrefix("merge"),
		silog.LevelInfo,
	)
	defer flushOutput()

	return r.Runner.Run(ctx, &scriptrun.RunRequest{
		Script: script,
		Env:    env,
		Stdout: output,
		Stderr: output,
	})
}

func (r *commandRunner) environment(
	ctx context.Context,
	item *mergeItem,
) ([]string, error) {
	common := map[string]string{
		"GIT_SPICE_FORGE_ID":     r.ForgeID,
		"GIT_SPICE_BRANCH":       item.branch,
		"GIT_SPICE_BASE_BRANCH":  item.base,
		"GIT_SPICE_TRUNK_BRANCH": r.Trunk,
		"GIT_SPICE_CHANGE_URL":   item.mergeURL,
		"GIT_SPICE_HEAD_SHA":     item.headHash.String(),
	}

	forgeEnv, err := r.Repository.CommandEnvironment(ctx, item.changeID)
	if err != nil {
		return nil, fmt.Errorf("forge environment: %w", err)
	}
	for key, value := range forgeEnv {
		if _, blocked := common[key]; blocked {
			continue
		}
		common[key] = value
	}

	env := make([]string, 0, len(common))
	for key, value := range common {
		env = append(env, key+"="+value)
	}
	slices.Sort(env)
	return env, nil
}

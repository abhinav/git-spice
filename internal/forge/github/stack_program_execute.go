package github

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/gateway/github"
)

// executeGitHubStackProgram runs mutations sequentially
// and joins their errors in encounter order.
// Successful writes are not rolled back when a later instruction fails.
func executeGitHubStackProgram(
	ctx context.Context,
	repository *Repository,
	program githubStackProgram,
) error {
	e := githubStackExecutor{repository: repository}
	if err := e.sequence(ctx, program); err != nil {
		e.errs = append(e.errs, err)
	}
	return errors.Join(e.errs...)
}

// githubStackExecutor owns the repository and caught errors for one execution.
// It reads the immutable program without changing its instructions.
type githubStackExecutor struct {
	repository *Repository // required

	// While sequence runs, errs holds failures caught by Try, in encounter order.
	// These failures must not escape through sequence's return value,
	// or an enclosing sequence would stop after the Try completes.
	errs []error
}

// sequence stops at the first uncaught failure.
// Try records its body's failure and lets the enclosing sequence continue.
func (e *githubStackExecutor) sequence(
	ctx context.Context,
	instructions []githubStackInstruction,
) error {
	for _, instruction := range instructions {
		var err error
		switch instruction := instruction.(type) {
		case githubStackTry:
			// Replacing one stack with paths A and B produces:
			// Try{Unstack, Try{ChangeBaseA, CreateA}, Try{CreateB}}.
			// Unstack failure skips both paths because they depend on dissolution.
			// ChangeBaseA failure skips CreateA, but is caught before CreateB runs.
			// That caught failure still belongs in the final result;
			// returning it here would also abort the enclosing sequence,
			// incorrectly treating CreateB as dependent on ChangeBaseA.
			if err := e.sequence(ctx, instruction.Body); err != nil {
				e.errs = append(e.errs, err)
			}
		case githubStackUnstack:
			err = e.unstack(ctx, instruction)
		case githubStackChangeBase:
			err = e.changeBase(ctx, instruction)
		case githubStackCreate:
			err = e.create(ctx, instruction)
		case githubStackAppend:
			err = e.append(ctx, instruction)
		default:
			panic(fmt.Sprintf("unexpected GitHub stack instruction %T", instruction))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *githubStackExecutor) unstack(ctx context.Context, instruction githubStackUnstack) error {
	result, err := e.repository.gateway.UnstackPullRequestStack(ctx, &github.UnstackPullRequestStackInput{
		Owner:       e.repository.owner,
		Repo:        e.repository.repo,
		StackNumber: instruction.StackNumber,
	})
	if err != nil {
		return fmt.Errorf("unstack GitHub native stack #%d: %w", instruction.StackNumber, err)
	}
	// GitHub can successfully unstack only part of a resource:
	// it releases eligible members but retains merged or locked members.
	// The compiled replacement assumes complete dissolution, not an arbitrary
	// released subset, so API success alone does not satisfy its prerequisite.
	// Report retained membership as failure before dependent writes can run;
	// any members already released remain released.
	if len(result.RemainingPullRequests) > 0 {
		return fmt.Errorf(
			"unstack GitHub native stack #%d: GitHub retained pull requests %v",
			instruction.StackNumber,
			result.RemainingPullRequests,
		)
	}
	return nil
}

func (e *githubStackExecutor) changeBase(ctx context.Context, instruction githubStackChangeBase) error {
	if err := e.repository.gateway.UpdatePullRequest(ctx, &github.UpdatePullRequestInput{
		PullRequestID: instruction.PullRequestID,
		BaseRefName:   &instruction.BaseBranch,
	}); err != nil {
		return fmt.Errorf(
			"update GitHub pull request #%d base: %w",
			instruction.PullRequestNumber,
			err,
		)
	}
	return nil
}

func (e *githubStackExecutor) create(ctx context.Context, instruction githubStackCreate) error {
	if err := e.repository.gateway.CreatePullRequestStack(ctx, &github.CreatePullRequestStackInput{
		Owner:        e.repository.owner,
		Repo:         e.repository.repo,
		PullRequests: instruction.PullRequests,
	}); err != nil {
		return fmt.Errorf("create GitHub native stack: %w", err)
	}
	return nil
}

func (e *githubStackExecutor) append(ctx context.Context, instruction githubStackAppend) error {
	if err := e.repository.gateway.AddPullRequestsToStack(ctx, &github.AddPullRequestsToStackInput{
		Owner:        e.repository.owner,
		Repo:         e.repository.repo,
		StackNumber:  instruction.StackNumber,
		PullRequests: instruction.PullRequests,
	}); err != nil {
		return fmt.Errorf("extend GitHub native stack #%d: %w", instruction.StackNumber, err)
	}
	return nil
}

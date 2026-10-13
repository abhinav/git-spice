package github

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/gateway/github"
)

func TestReconcileGitHubStacks(t *testing.T) {
	openChange := func(
		number int,
		baseNumber int,
		desiredBase string,
		currentBase string,
		stack *github.PullRequestStack,
	) githubStackChange {
		return githubStackChange{
			number:           number,
			baseNumber:       baseNumber,
			baseBranch:       desiredBase,
			headInRepository: true,
			pullRequest: &github.StackUpdatePullRequest{
				ID:          github.ID(fmt.Sprintf("PR_%d", number)),
				State:       github.PullRequestStateOpen,
				BaseRefName: currentBase,
				Stack:       stack,
			},
		}
	}
	mergedChange := func(
		number int,
		baseNumber int,
		desiredBase string,
		currentBase string,
	) githubStackChange {
		change := openChange(number, baseNumber, desiredBase, currentBase, nil)
		change.pullRequest.State = github.PullRequestStateMerged
		return change
	}
	nativeStack := func(number int, members ...int) *github.PullRequestStack {
		stack := &github.PullRequestStack{Number: number}
		for _, member := range members {
			stack.Members = append(stack.Members, github.PullRequestStackMember{
				Number: member,
				State:  github.PullRequestStateOpen,
			})
		}
		return stack
	}

	tests := []struct {
		name string
		give []githubStackChange
		want []githubStackTransition
	}{
		{
			name: "Current",
			give: []githubStackChange{
				openChange(1, 0, "main", "main", nativeStack(42, 1, 2)),
				openChange(2, 1, "a", "a", nativeStack(42, 1, 2)),
			},
		},
		{
			name: "SinglePullRequestNeedsNoNativeStack",
			give: []githubStackChange{
				openChange(1, 0, "main", "main", nil),
			},
		},
		{
			name: "Create",
			give: []githubStackChange{
				openChange(1, 0, "main", "main", nil),
				openChange(2, 1, "a", "a", nil),
			},
			want: []githubStackTransition{{
				paths: []githubStackPathTransition{{
					pullRequests: []int{1, 2},
				}},
			}},
		},
		{
			name: "Append",
			give: []githubStackChange{
				openChange(1, 0, "main", "main", nativeStack(42, 1, 2)),
				openChange(2, 1, "a", "a", nativeStack(42, 1, 2)),
				openChange(3, 2, "b", "a", nil),
			},
			want: []githubStackTransition{{
				paths: []githubStackPathTransition{{
					baseUpdates: []githubPullRequestBaseUpdate{{
						pullRequestNumber: 3,
						pullRequestID:     "PR_3",
						baseBranch:        "b",
					}},
					stackNumber:  42,
					pullRequests: []int{3},
				}},
			}},
		},
		{
			// Desired: 1 -> 3 -> 2. Current native stack: 1 -> 2.
			name: "Insert",
			give: []githubStackChange{
				openChange(1, 0, "main", "main", nativeStack(42, 1, 2)),
				openChange(3, 1, "a", "a", nil),
				openChange(2, 3, "c", "a", nativeStack(42, 1, 2)),
			},
			want: []githubStackTransition{{
				unstackNumber: 42,
				paths: []githubStackPathTransition{{
					baseUpdates: []githubPullRequestBaseUpdate{{
						pullRequestNumber: 2,
						pullRequestID:     "PR_2",
						baseBranch:        "c",
					}},
					pullRequests: []int{1, 3, 2},
				}},
			}},
		},
		{
			// Desired: 1 -> 3. Current native stack: 1 -> 2 -> 3.
			name: "Remove",
			give: []githubStackChange{
				openChange(1, 0, "main", "main", nativeStack(42, 1, 2, 3)),
				openChange(3, 1, "a", "b", nativeStack(42, 1, 2, 3)),
			},
			want: []githubStackTransition{{
				unstackNumber: 42,
				paths: []githubStackPathTransition{{
					baseUpdates: []githubPullRequestBaseUpdate{{
						pullRequestNumber: 3,
						pullRequestID:     "PR_3",
						baseBranch:        "a",
					}},
					pullRequests: []int{1, 3},
				}},
			}},
		},
		{
			// Desired: 1 and 2 are separate roots. Current native stack: 1 -> 2.
			name: "Split",
			give: []githubStackChange{
				openChange(1, 0, "main", "main", nativeStack(42, 1, 2)),
				openChange(2, 0, "main", "a", nativeStack(42, 1, 2)),
			},
			want: []githubStackTransition{{
				unstackNumber: 42,
				paths: []githubStackPathTransition{{
					baseUpdates: []githubPullRequestBaseUpdate{{
						pullRequestNumber: 2,
						pullRequestID:     "PR_2",
						baseBranch:        "main",
					}},
				}},
			}},
		},
		{
			// Desired: 1 -> 2 and 3 -> 4. Current native stack: 1 -> 2 -> 3 -> 4.
			name: "SplitIntoStacks",
			give: []githubStackChange{
				openChange(1, 0, "main", "main", nativeStack(42, 1, 2, 3, 4)),
				openChange(2, 1, "a", "a", nativeStack(42, 1, 2, 3, 4)),
				openChange(3, 0, "main", "b", nativeStack(42, 1, 2, 3, 4)),
				openChange(4, 3, "c", "c", nativeStack(42, 1, 2, 3, 4)),
			},
			want: []githubStackTransition{{
				unstackNumber: 42,
				paths: []githubStackPathTransition{
					{pullRequests: []int{1, 2}},
					{
						baseUpdates: []githubPullRequestBaseUpdate{{
							pullRequestNumber: 3,
							pullRequestID:     "PR_3",
							baseBranch:        "main",
						}},
						pullRequests: []int{3, 4},
					},
				},
			}},
		},
		{
			name: "ReconnectAboveMergedChange",
			give: []githubStackChange{
				mergedChange(1, 0, "main", "main"),
				openChange(2, 1, "a", "a", nil),
				openChange(3, 2, "b", "b", nil),
			},
			want: []githubStackTransition{{
				paths: []githubStackPathTransition{{
					pullRequests: []int{2, 3},
				}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconcileGitHubStacks(tt.give)

			assert.Equal(t, tt.want, got.transitions)
			assert.Empty(t, got.warnings)
			assert.Empty(t, got.errs)
		})
	}
}

func TestReconcileGitHubStacks_preservesInput(t *testing.T) {
	// Replacing 1 -> 2 with 1 -> 3 -> 2 must not modify the facts used to plan it.
	stack := &github.PullRequestStack{
		Number: 42,
		Members: []github.PullRequestStackMember{
			{Number: 1, State: github.PullRequestStateOpen},
			{Number: 2, State: github.PullRequestStateOpen},
		},
	}
	give := []githubStackChange{
		{
			number: 1, baseBranch: "main", headInRepository: true,
			pullRequest: &github.StackUpdatePullRequest{
				ID: "PR_1", State: github.PullRequestStateOpen, BaseRefName: "main", Stack: stack,
			},
		},
		{
			number: 3, baseNumber: 1, baseBranch: "a", headInRepository: true,
			pullRequest: &github.StackUpdatePullRequest{
				ID: "PR_3", State: github.PullRequestStateOpen, BaseRefName: "a",
			},
		},
		{
			number: 2, baseNumber: 3, baseBranch: "c", headInRepository: true,
			pullRequest: &github.StackUpdatePullRequest{
				ID: "PR_2", State: github.PullRequestStateOpen, BaseRefName: "a", Stack: stack,
			},
		},
	}

	before := slices.Clone(give)
	for i, change := range give {
		pullRequest := *change.pullRequest
		before[i].pullRequest = &pullRequest
		if pullRequest.Stack != nil {
			stack := *pullRequest.Stack
			stack.Members = slices.Clone(stack.Members)
			before[i].pullRequest.Stack = &stack
		}
	}

	first := reconcileGitHubStacks(give)
	second := reconcileGitHubStacks(give)

	assert.Equal(t, before, give)
	assert.Equal(t, first, second)
	assert.Empty(t, first.warnings)
	assert.Empty(t, first.errs)
}

func TestReconcileGitHubStacks_errors(t *testing.T) {
	openChange := func(
		number int,
		baseNumber int,
		desiredBase string,
		currentBase string,
		stack *github.PullRequestStack,
	) githubStackChange {
		return githubStackChange{
			number:           number,
			baseNumber:       baseNumber,
			baseBranch:       desiredBase,
			headInRepository: true,
			pullRequest: &github.StackUpdatePullRequest{
				ID:          github.ID(fmt.Sprintf("PR_%d", number)),
				State:       github.PullRequestStateOpen,
				BaseRefName: currentBase,
				Stack:       stack,
			},
		}
	}
	missingChange := func(
		number int,
		baseNumber int,
		desiredBase string,
	) githubStackChange {
		return githubStackChange{
			number:           number,
			baseNumber:       baseNumber,
			baseBranch:       desiredBase,
			headInRepository: true,
		}
	}
	nativeStack := func(number int, members ...int) *github.PullRequestStack {
		stack := &github.PullRequestStack{Number: number}
		for _, member := range members {
			stack.Members = append(stack.Members, github.PullRequestStackMember{
				Number: member,
				State:  github.PullRequestStateOpen,
			})
		}
		return stack
	}
	lockedStack := func(number int, members ...int) *github.PullRequestStack {
		stack := nativeStack(number, members...)
		stack.Members[len(stack.Members)-1].Locked = true
		return stack
	}
	errorMessages := func(errs []error) []string {
		var messages []string
		for _, err := range errs {
			messages = append(messages, err.Error())
		}
		return messages
	}

	tests := []struct {
		name string
		give []githubStackChange

		wantTransitions []githubStackTransition
		wantWarnings    []string
		wantErrors      []string
	}{
		{
			// 1 -+-> 2 -> 5
			//    `-> 3 -> 4
			name: "DivergentLongestPath",
			give: []githubStackChange{
				openChange(1, 0, "base", "base", nil),
				openChange(2, 1, "base", "base", nil),
				openChange(3, 1, "base", "base", nil),
				openChange(4, 3, "base", "base", nil),
				openChange(5, 2, "base", "base", nil),
			},
			wantTransitions: []githubStackTransition{{
				paths: []githubStackPathTransition{{
					pullRequests: []int{1, 3, 4},
				}},
			}},
			wantWarnings: []string{
				"#2: Leaving pull request and its upstack out of the GitHub native stack: the change tree diverges from the selected linear path",
			},
		},
		{
			// 1 -> 2 -+-> 3 -> 5
			//         `-> 4 (existing native stack)
			name: "PreferExistingStack",
			give: []githubStackChange{
				openChange(1, 0, "base", "base", nativeStack(42, 1, 2, 4)),
				openChange(2, 1, "base", "base", nativeStack(42, 1, 2, 4)),
				openChange(3, 2, "base", "base", nil),
				openChange(4, 2, "base", "base", nativeStack(42, 1, 2, 4)),
				openChange(5, 3, "base", "base", nil),
			},
			wantWarnings: []string{
				"#3: Leaving pull request and its upstack out of the GitHub native stack: the change tree diverges from the selected linear path",
			},
		},
		{
			// Desired: 1 -> {2, 3}. Current native stack references absent #4.
			name: "LeaveIncompatibleDivergentStack",
			give: []githubStackChange{
				openChange(1, 0, "base", "base", nativeStack(42, 1, 4)),
				openChange(2, 1, "base", "base", nil),
				openChange(3, 1, "base", "base", nil),
			},
			wantWarnings: []string{
				"#1: Leaving pull request and its upstack in existing GitHub native stack #42: open pull requests #1, #4 have incompatible membership",
			},
		},
		{
			name: "LeaveLockedReplacement",
			give: []githubStackChange{
				openChange(1, 0, "main", "main", lockedStack(42, 1, 2)),
				openChange(3, 1, "a", "a", nil),
				openChange(2, 3, "c", "a", lockedStack(42, 1, 2)),
			},
			wantWarnings: []string{
				"#1: Leaving pull request and its upstack in existing GitHub native stack #42: merged, queued, or auto-merge pull requests prevent restructuring",
			},
		},
		{
			name: "LeaveReplacementWithMergedMember",
			give: func() []githubStackChange {
				stack := &github.PullRequestStack{
					Number: 42,
					Members: []github.PullRequestStackMember{
						{Number: 1, State: github.PullRequestStateMerged},
						{Number: 2, State: github.PullRequestStateOpen},
					},
				}
				merged := openChange(1, 0, "main", "main", stack)
				merged.pullRequest.State = github.PullRequestStateMerged
				return []githubStackChange{
					merged,
					openChange(3, 1, "a", "a", nil),
					openChange(2, 3, "c", "a", stack),
				}
			}(),
			wantWarnings: []string{
				"#3: Leaving pull request and its upstack in existing GitHub native stack #42: merged, queued, or auto-merge pull requests prevent restructuring",
			},
		},
		{
			name: "MissingPullRequest",
			give: []githubStackChange{
				missingChange(1, 0, "main"),
			},
			wantWarnings: []string{
				"#1: Leaving pull request and its upstack out of the GitHub native stack: the pull request was not found",
			},
			wantErrors: []string{"inspect GitHub pull request #1: not found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconcileGitHubStacks(tt.give)

			assert.Equal(t, tt.wantTransitions, got.transitions)
			assert.Equal(t, tt.wantWarnings, got.warnings)
			assert.Equal(t, tt.wantErrors, errorMessages(got.errs))
		})
	}
}

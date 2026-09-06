package github

import (
	"fmt"
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
	) githubStackSnapshotChange {
		return githubStackSnapshotChange{
			desired: githubStackDesiredChange{
				number:     number,
				baseNumber: baseNumber,
				baseBranch: desiredBase,
			},
			pullRequest: &githubStackPullRequestState{
				id:               github.ID(fmt.Sprintf("PR_%d", number)),
				state:            github.PullRequestStateOpen,
				baseBranch:       currentBase,
				headInRepository: true,
				stack:            stack,
			},
		}
	}
	mergedChange := func(
		number int,
		baseNumber int,
		desiredBase string,
		currentBase string,
	) githubStackSnapshotChange {
		change := openChange(number, baseNumber, desiredBase, currentBase, nil)
		change.pullRequest.state = github.PullRequestStateMerged
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
		give githubStackSnapshot
		want []githubStackTransition
	}{
		{
			name: "Current",
			give: githubStackSnapshot{
				openChange(1, 0, "main", "main", nativeStack(42, 1, 2)),
				openChange(2, 1, "a", "a", nativeStack(42, 1, 2)),
			},
		},
		{
			name: "SinglePullRequestNeedsNoNativeStack",
			give: githubStackSnapshot{
				openChange(1, 0, "main", "main", nil),
			},
		},
		{
			name: "Create",
			give: githubStackSnapshot{
				openChange(1, 0, "main", "main", nil),
				openChange(2, 1, "a", "a", nil),
			},
			want: []githubStackTransition{{
				paths: []githubStackPathTransition{{
					stackUpdate: &githubStackMembershipUpdate{
						pullRequests: []int{1, 2},
					},
				}},
			}},
		},
		{
			name: "Append",
			give: githubStackSnapshot{
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
					stackUpdate: &githubStackMembershipUpdate{
						stackNumber:  42,
						pullRequests: []int{3},
					},
				}},
			}},
		},
		{
			// Desired: 1 -> 3 -> 2. Current native stack: 1 -> 2.
			name: "Insert",
			give: githubStackSnapshot{
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
					stackUpdate: &githubStackMembershipUpdate{
						pullRequests: []int{1, 3, 2},
					},
				}},
			}},
		},
		{
			// Desired: 1 -> 3. Current native stack: 1 -> 2 -> 3.
			name: "Remove",
			give: githubStackSnapshot{
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
					stackUpdate: &githubStackMembershipUpdate{
						pullRequests: []int{1, 3},
					},
				}},
			}},
		},
		{
			// Desired: 1 and 2 are separate roots. Current native stack: 1 -> 2.
			name: "Split",
			give: githubStackSnapshot{
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
			name: "ReconnectAboveMergedChange",
			give: githubStackSnapshot{
				mergedChange(1, 0, "main", "main"),
				openChange(2, 1, "a", "a", nil),
				openChange(3, 2, "b", "b", nil),
			},
			want: []githubStackTransition{{
				paths: []githubStackPathTransition{{
					stackUpdate: &githubStackMembershipUpdate{
						pullRequests: []int{2, 3},
					},
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

func TestReconcileGitHubStacks_errors(t *testing.T) {
	openChange := func(
		number int,
		baseNumber int,
		desiredBase string,
		currentBase string,
		stack *github.PullRequestStack,
	) githubStackSnapshotChange {
		return githubStackSnapshotChange{
			desired: githubStackDesiredChange{
				number:     number,
				baseNumber: baseNumber,
				baseBranch: desiredBase,
			},
			pullRequest: &githubStackPullRequestState{
				id:               github.ID(fmt.Sprintf("PR_%d", number)),
				state:            github.PullRequestStateOpen,
				baseBranch:       currentBase,
				headInRepository: true,
				stack:            stack,
			},
		}
	}
	missingChange := func(
		number int,
		baseNumber int,
		desiredBase string,
	) githubStackSnapshotChange {
		return githubStackSnapshotChange{
			desired: githubStackDesiredChange{
				number:     number,
				baseNumber: baseNumber,
				baseBranch: desiredBase,
			},
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
		give githubStackSnapshot

		wantTransitions []githubStackTransition
		wantWarnings    []string
		wantErrors      []string
	}{
		{
			// 1 -+-> 2 -> 5
			//    `-> 3 -> 4
			name: "DivergentLongestPath",
			give: githubStackSnapshot{
				openChange(1, 0, "base", "base", nil),
				openChange(2, 1, "base", "base", nil),
				openChange(3, 1, "base", "base", nil),
				openChange(4, 3, "base", "base", nil),
				openChange(5, 2, "base", "base", nil),
			},
			wantTransitions: []githubStackTransition{{
				paths: []githubStackPathTransition{{
					stackUpdate: &githubStackMembershipUpdate{
						pullRequests: []int{1, 3, 4},
					},
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
			give: githubStackSnapshot{
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
			give: githubStackSnapshot{
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
			give: githubStackSnapshot{
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
			give: func() githubStackSnapshot {
				stack := &github.PullRequestStack{
					Number: 42,
					Members: []github.PullRequestStackMember{
						{Number: 1, State: github.PullRequestStateMerged},
						{Number: 2, State: github.PullRequestStateOpen},
					},
				}
				merged := openChange(1, 0, "main", "main", stack)
				merged.pullRequest.state = github.PullRequestStateMerged
				return githubStackSnapshot{
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
			give: githubStackSnapshot{
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

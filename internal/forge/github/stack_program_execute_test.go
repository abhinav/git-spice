package github

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.uber.org/mock/gomock"
)

func TestExecuteGitHubStackProgram_stopsAfterPrerequisiteFailure(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gomock.InOrder(
		gateway.EXPECT().UnstackPullRequestStack(gomock.Any(), &github.UnstackPullRequestStackInput{
			Owner: "acme", Repo: "repo", StackNumber: 42,
		}).Return(nil, github.ErrForbidden),
		gateway.EXPECT().AddPullRequestsToStack(gomock.Any(), &github.AddPullRequestsToStackInput{
			Owner: "acme", Repo: "repo", StackNumber: 99, PullRequests: []int{10},
		}).Return(nil),
	)
	program := githubStackProgram{
		githubStackTry{Body: []githubStackInstruction{
			githubStackUnstack{StackNumber: 42},
			githubStackTry{Body: []githubStackInstruction{
				githubStackCreate{PullRequests: []int{1, 2}},
			}},
			githubStackTry{Body: []githubStackInstruction{
				githubStackCreate{PullRequests: []int{3, 4}},
			}},
		}},
		githubStackTry{Body: []githubStackInstruction{
			githubStackAppend{StackNumber: 99, PullRequests: []int{10}},
		}},
	}
	repository := &Repository{
		owner:   "acme",
		repo:    "repo",
		gateway: gateway,
		log:     silogtest.New(t),
	}

	err := executeGitHubStackProgram(t.Context(), repository, program)
	require.ErrorIs(t, err, github.ErrForbidden)
}

func TestExecuteGitHubStackProgram_continuesIndependentPath(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().UpdatePullRequest(gomock.Any(), &github.UpdatePullRequestInput{
		PullRequestID: "PR_2",
		BaseRefName:   new("a"),
	}).Return(github.ErrForbidden)
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), &github.CreatePullRequestStackInput{
		Owner:        "acme",
		Repo:         "repo",
		PullRequests: []int{10, 11},
	}).Return(nil)
	program := githubStackProgram{
		githubStackTry{Body: []githubStackInstruction{
			githubStackTry{Body: []githubStackInstruction{
				githubStackChangeBase{
					PullRequestNumber: 2,
					PullRequestID:     "PR_2",
					BaseBranch:        "a",
				},
				githubStackCreate{PullRequests: []int{1, 2}},
			}},
			githubStackTry{Body: []githubStackInstruction{
				githubStackCreate{PullRequests: []int{10, 11}},
			}},
		}},
	}
	repository := &Repository{
		owner:   "acme",
		repo:    "repo",
		gateway: gateway,
		log:     silogtest.New(t),
	}

	err := executeGitHubStackProgram(t.Context(), repository, program)
	require.ErrorIs(t, err, github.ErrForbidden)
}

func TestExecuteGitHubStackProgram_retainedMembersBlockReplacement(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().UnstackPullRequestStack(
		gomock.Any(),
		&github.UnstackPullRequestStackInput{
			Owner: "acme", Repo: "repo", StackNumber: 42,
		},
	).Return(&github.UnstackPullRequestStackResult{RemainingPullRequests: []int{1, 2}}, nil)
	program := githubStackProgram{
		githubStackTry{Body: []githubStackInstruction{
			githubStackUnstack{StackNumber: 42, MergedPullRequests: []int{1}},
			githubStackTry{Body: []githubStackInstruction{
				githubStackChangeBase{PullRequestNumber: 2, PullRequestID: "PR_2", BaseBranch: "main"},
				githubStackCreate{PullRequests: []int{2, 3}},
			}},
			githubStackTry{Body: []githubStackInstruction{
				githubStackCreate{PullRequests: []int{4, 5}},
			}},
		}},
	}
	repository := &Repository{
		owner: "acme", repo: "repo", gateway: gateway, log: silogtest.New(t),
	}

	err := executeGitHubStackProgram(t.Context(), repository, program)
	require.EqualError(t, err, "unstack GitHub native stack #42: GitHub retained pull requests [2]")
}

func TestExecuteGitHubStackProgram_nestedFailuresDoNotEscapeTry(t *testing.T) {
	baseErr := errors.New("base rejected")
	createErr := errors.New("creation rejected")
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gomock.InOrder(
		gateway.EXPECT().UnstackPullRequestStack(gomock.Any(), &github.UnstackPullRequestStackInput{
			Owner: "acme", Repo: "repo", StackNumber: 42,
		}).Return(&github.UnstackPullRequestStackResult{}, nil),
		gateway.EXPECT().UpdatePullRequest(gomock.Any(), &github.UpdatePullRequestInput{
			PullRequestID: "PR_2", BaseRefName: new("a"),
		}).Return(baseErr),
		gateway.EXPECT().CreatePullRequestStack(gomock.Any(), &github.CreatePullRequestStackInput{
			Owner: "acme", Repo: "repo", PullRequests: []int{3, 4},
		}).Return(createErr),
		gateway.EXPECT().AddPullRequestsToStack(gomock.Any(), &github.AddPullRequestsToStackInput{
			Owner: "acme", Repo: "repo", StackNumber: 88, PullRequests: []int{5},
		}).Return(nil),
		gateway.EXPECT().AddPullRequestsToStack(gomock.Any(), &github.AddPullRequestsToStackInput{
			Owner: "acme", Repo: "repo", StackNumber: 99, PullRequests: []int{10},
		}).Return(nil),
	)
	program := githubStackProgram{
		githubStackTry{Body: []githubStackInstruction{
			githubStackUnstack{StackNumber: 42},
			githubStackTry{Body: []githubStackInstruction{
				githubStackChangeBase{PullRequestNumber: 2, PullRequestID: "PR_2", BaseBranch: "a"},
				githubStackCreate{PullRequests: []int{1, 2}},
			}},
			githubStackTry{Body: []githubStackInstruction{
				githubStackCreate{PullRequests: []int{3, 4}},
			}},
			// Both inner failures are caught, so this enclosing sequence continues.
			githubStackAppend{StackNumber: 88, PullRequests: []int{5}},
		}},
		githubStackTry{Body: []githubStackInstruction{
			githubStackAppend{StackNumber: 99, PullRequests: []int{10}},
		}},
	}
	repository := &Repository{
		owner: "acme", repo: "repo", gateway: gateway, log: silogtest.New(t),
	}

	err := executeGitHubStackProgram(t.Context(), repository, program)
	require.ErrorIs(t, err, baseErr)
	require.ErrorIs(t, err, createErr)
	assert.EqualError(t, err, "update GitHub pull request #2 base: base rejected\ncreate GitHub native stack: creation rejected")
}

func TestExecuteGitHubStackProgram_sequenceStopsAtFirstFailure(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().UpdatePullRequest(gomock.Any(), &github.UpdatePullRequestInput{
		PullRequestID: "PR_2", BaseRefName: new("a"),
	}).Return(github.ErrForbidden)
	program := githubStackProgram{
		githubStackChangeBase{PullRequestNumber: 2, PullRequestID: "PR_2", BaseBranch: "a"},
		githubStackCreate{PullRequests: []int{1, 2}},
	}
	repository := &Repository{
		owner: "acme", repo: "repo", gateway: gateway, log: silogtest.New(t),
	}

	err := executeGitHubStackProgram(t.Context(), repository, program)
	require.ErrorIs(t, err, github.ErrForbidden)
}

func TestExecuteGitHubStackProgram_keepsSuccessfulWritesAfterFailure(t *testing.T) {
	model := &stackResourceModel{
		pullRequests: map[int]*stackResourceModelPR{
			1: {head: "a", base: "main", state: github.PullRequestStateOpen, headInRepository: true},
			2: {head: "b", base: "a", state: github.PullRequestStateOpen, headInRepository: true},
		},
		stacks: make(map[int][]int),
	}
	program := githubStackProgram{
		githubStackTry{Body: []githubStackInstruction{
			githubStackChangeBase{PullRequestNumber: 2, PullRequestID: "2", BaseBranch: "main"},
			// This membership write fails because #2 no longer targets #1.
			githubStackCreate{PullRequests: []int{1, 2}},
		}},
	}

	err := executeGitHubStackProgram(t.Context(), &Repository{
		owner: "acme", repo: "repo", gateway: model, log: silogtest.New(t),
	}, program)
	require.Error(t, err)
	assert.Equal(t, "main", model.pullRequests[2].base)
	assert.Empty(t, model.stacks)
	assert.Equal(t, []string{`retarget 2 to "main"`, "create [1 2]"}, model.trace)
}

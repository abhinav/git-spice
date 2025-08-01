// Code generated by MockGen. DO NOT EDIT.
// Source: go.abhg.dev/gs/internal/handler/track (interfaces: GitRepository,Service)
//
// Generated by this command:
//
//	mockgen -destination mocks_test.go -package track -typed . GitRepository,Service
//

// Package track is a generated GoMock package.
package track

import (
	context "context"
	iter "iter"
	reflect "reflect"

	git "go.abhg.dev/gs/internal/git"
	gomock "go.uber.org/mock/gomock"
)

// MockGitRepository is a mock of GitRepository interface.
type MockGitRepository struct {
	ctrl     *gomock.Controller
	recorder *MockGitRepositoryMockRecorder
	isgomock struct{}
}

// MockGitRepositoryMockRecorder is the mock recorder for MockGitRepository.
type MockGitRepositoryMockRecorder struct {
	mock *MockGitRepository
}

// NewMockGitRepository creates a new mock instance.
func NewMockGitRepository(ctrl *gomock.Controller) *MockGitRepository {
	mock := &MockGitRepository{ctrl: ctrl}
	mock.recorder = &MockGitRepositoryMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockGitRepository) EXPECT() *MockGitRepositoryMockRecorder {
	return m.recorder
}

// ListCommits mocks base method.
func (m *MockGitRepository) ListCommits(ctx context.Context, commits git.CommitRange) iter.Seq2[git.Hash, error] {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListCommits", ctx, commits)
	ret0, _ := ret[0].(iter.Seq2[git.Hash, error])
	return ret0
}

// ListCommits indicates an expected call of ListCommits.
func (mr *MockGitRepositoryMockRecorder) ListCommits(ctx, commits any) *MockGitRepositoryListCommitsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListCommits", reflect.TypeOf((*MockGitRepository)(nil).ListCommits), ctx, commits)
	return &MockGitRepositoryListCommitsCall{Call: call}
}

// MockGitRepositoryListCommitsCall wrap *gomock.Call
type MockGitRepositoryListCommitsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockGitRepositoryListCommitsCall) Return(arg0 iter.Seq2[git.Hash, error]) *MockGitRepositoryListCommitsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockGitRepositoryListCommitsCall) Do(f func(context.Context, git.CommitRange) iter.Seq2[git.Hash, error]) *MockGitRepositoryListCommitsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockGitRepositoryListCommitsCall) DoAndReturn(f func(context.Context, git.CommitRange) iter.Seq2[git.Hash, error]) *MockGitRepositoryListCommitsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// PeelToCommit mocks base method.
func (m *MockGitRepository) PeelToCommit(ctx context.Context, ref string) (git.Hash, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PeelToCommit", ctx, ref)
	ret0, _ := ret[0].(git.Hash)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// PeelToCommit indicates an expected call of PeelToCommit.
func (mr *MockGitRepositoryMockRecorder) PeelToCommit(ctx, ref any) *MockGitRepositoryPeelToCommitCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PeelToCommit", reflect.TypeOf((*MockGitRepository)(nil).PeelToCommit), ctx, ref)
	return &MockGitRepositoryPeelToCommitCall{Call: call}
}

// MockGitRepositoryPeelToCommitCall wrap *gomock.Call
type MockGitRepositoryPeelToCommitCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockGitRepositoryPeelToCommitCall) Return(arg0 git.Hash, arg1 error) *MockGitRepositoryPeelToCommitCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockGitRepositoryPeelToCommitCall) Do(f func(context.Context, string) (git.Hash, error)) *MockGitRepositoryPeelToCommitCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockGitRepositoryPeelToCommitCall) DoAndReturn(f func(context.Context, string) (git.Hash, error)) *MockGitRepositoryPeelToCommitCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockService is a mock of Service interface.
type MockService struct {
	ctrl     *gomock.Controller
	recorder *MockServiceMockRecorder
	isgomock struct{}
}

// MockServiceMockRecorder is the mock recorder for MockService.
type MockServiceMockRecorder struct {
	mock *MockService
}

// NewMockService creates a new mock instance.
func NewMockService(ctrl *gomock.Controller) *MockService {
	mock := &MockService{ctrl: ctrl}
	mock.recorder = &MockServiceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockService) EXPECT() *MockServiceMockRecorder {
	return m.recorder
}

// VerifyRestacked mocks base method.
func (m *MockService) VerifyRestacked(ctx context.Context, name string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "VerifyRestacked", ctx, name)
	ret0, _ := ret[0].(error)
	return ret0
}

// VerifyRestacked indicates an expected call of VerifyRestacked.
func (mr *MockServiceMockRecorder) VerifyRestacked(ctx, name any) *MockServiceVerifyRestackedCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VerifyRestacked", reflect.TypeOf((*MockService)(nil).VerifyRestacked), ctx, name)
	return &MockServiceVerifyRestackedCall{Call: call}
}

// MockServiceVerifyRestackedCall wrap *gomock.Call
type MockServiceVerifyRestackedCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockServiceVerifyRestackedCall) Return(arg0 error) *MockServiceVerifyRestackedCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockServiceVerifyRestackedCall) Do(f func(context.Context, string) error) *MockServiceVerifyRestackedCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockServiceVerifyRestackedCall) DoAndReturn(f func(context.Context, string) error) *MockServiceVerifyRestackedCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

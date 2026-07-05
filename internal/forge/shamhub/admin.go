package shamhub

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"time"

	"go.abhg.dev/gs/internal/forge"
)

var (
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/users",
		(*ShamHub).handleAdminRegisterUser,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/repos",
		(*ShamHub).handleAdminNewRepository,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/repos/fork",
		(*ShamHub).handleAdminForkRepository,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/config",
		(*ShamHub).handleAdminConfig,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/changes/{owner}/{repo}/{number}/merge",
		(*ShamHub).handleAdminMergeChange,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/changes/{owner}/{repo}/{number}/reject",
		(*ShamHub).handleAdminRejectChange,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/changes/{owner}/{repo}/{number}/checks",
		(*ShamHub).handleAdminSetStatus,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/changes/{owner}/{repo}/{number}/mergeability",
		(*ShamHub).handleAdminSetMergeability,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/comments",
		(*ShamHub).handleAdminPostComment,
	)
	_ = shamhubRESTHandler(
		"PATCH /_shamhub/admin/comments/{id}",
		(*ShamHub).handleAdminEditComment,
	)
	_ = shamhubRESTHandler(
		"DELETE /_shamhub/admin/comments/{id}",
		(*ShamHub).handleAdminDeleteComment,
	)
	_ = shamhubRESTHandler(
		"GET /_shamhub/admin/dump/changes",
		(*ShamHub).handleAdminDumpChanges,
	)
	_ = shamhubRESTHandler(
		"GET /_shamhub/admin/dump/changes/{number}",
		(*ShamHub).handleAdminDumpChange,
	)
	_ = shamhubHTTPHandler(
		"GET /_shamhub/admin/dump/comments",
		(*ShamHub).handleAdminDumpComments,
	)
)

func shamhubHTTPHandler(
	pattern string,
	handler func(*ShamHub, http.ResponseWriter, *http.Request),
) struct{} {
	_handlers = append(_handlers, shamhubEndpoint{
		Pattern: pattern,
		Handler: func(sh *ShamHub, w http.ResponseWriter, r *http.Request) {
			handler(sh, w, r)
		},
	})
	return struct{}{}
}

type adminRegisterUserBody struct {
	Username string `json:"username"`
}

type adminRegisterUserRequest struct {
	Username string `json:"username"`
}

type adminRegisterUserResponse struct{}

// User administration creates identities that can later log in.
func (sh *ShamHub) handleAdminRegisterUser(
	_ context.Context,
	req adminRegisterUserRequest,
) (*adminRegisterUserResponse, error) {
	if err := sh.RegisterUser(req.Username); err != nil {
		return nil, err
	}
	return &adminRegisterUserResponse{}, nil
}

type adminNewRepositoryBody struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

type adminNewRepositoryRequest struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

type adminRepositoryResponse struct {
	URL string `json:"url"`
}

// Repository administration creates a bare Git repository and returns its URL.
func (sh *ShamHub) handleAdminNewRepository(
	_ context.Context,
	req adminNewRepositoryRequest,
) (*adminRepositoryResponse, error) {
	url, err := sh.NewRepository(req.Owner, req.Repo)
	if err != nil {
		return nil, err
	}
	return &adminRepositoryResponse{URL: url}, nil
}

type adminForkRepositoryBody struct {
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	ForkOwner string `json:"forkOwner"`
}

type adminForkRepositoryRequest struct {
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	ForkOwner string `json:"forkOwner"`
}

// Repository forking copies an existing bare repository to a new owner.
func (sh *ShamHub) handleAdminForkRepository(
	_ context.Context,
	req adminForkRepositoryRequest,
) (*adminRepositoryResponse, error) {
	url, err := sh.ForkRepository(req.Owner, req.Repo, req.ForkOwner)
	if err != nil {
		return nil, err
	}
	return &adminRepositoryResponse{URL: url}, nil
}

type adminConfigBody struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type adminConfigRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type adminConfigResponse struct{}

// Configuration updates mutate ShamHub server knobs used by tests.
func (sh *ShamHub) handleAdminConfig(
	_ context.Context,
	req adminConfigRequest,
) (*adminConfigResponse, error) {
	switch req.Key {
	case "changeTemplateErrorDelay":
		delay, err := time.ParseDuration(req.Value)
		if err != nil {
			return nil, badRequestErrorf("parse duration: %s", err)
		}

		sh.mu.Lock()
		sh.changeTemplateErrorDelay = delay
		sh.mu.Unlock()

	case "mergeMethod":
		mergeMethod, err := parseMergeMethod(req.Value)
		if err != nil {
			return nil, badRequestErrorf("%s", err)
		}

		sh.mu.Lock()
		sh.defaultMergeMethod = mergeMethod
		sh.mu.Unlock()

	default:
		return nil, badRequestErrorf("unknown config key %q", req.Key)
	}

	return &adminConfigResponse{}, nil
}

type adminMergeChangeBody struct {
	Time           time.Time `json:"time"`
	CommitterName  string    `json:"committerName,omitempty"`
	CommitterEmail string    `json:"committerEmail,omitempty"`
	DeleteBranch   bool      `json:"deleteBranch,omitempty"`
	Squash         bool      `json:"squash,omitempty"`
}

type adminMergeChangeRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Number int    `path:"number" json:"-"`

	Time           time.Time `json:"time"`
	CommitterName  string    `json:"committerName,omitempty"`
	CommitterEmail string    `json:"committerEmail,omitempty"`
	DeleteBranch   bool      `json:"deleteBranch,omitempty"`
	Squash         bool      `json:"squash,omitempty"`
}

type adminMergeChangeResponse struct{}

// Change merging drives the same merge operation that forge clients observe.
func (sh *ShamHub) handleAdminMergeChange(
	_ context.Context,
	req adminMergeChangeRequest,
) (*adminMergeChangeResponse, error) {
	if err := sh.MergeChange(MergeChangeRequest{
		Owner:          req.Owner,
		Repo:           req.Repo,
		Number:         req.Number,
		Time:           req.Time,
		CommitterName:  req.CommitterName,
		CommitterEmail: req.CommitterEmail,
		DeleteBranch:   req.DeleteBranch,
		Squash:         req.Squash,
	}); err != nil {
		return nil, err
	}
	return &adminMergeChangeResponse{}, nil
}

type adminRejectChangeRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Number int    `path:"number" json:"-"`
}

type adminRejectChangeBody struct{}

type adminRejectChangeResponse struct{}

// Change rejection closes a change without merging it.
func (sh *ShamHub) handleAdminRejectChange(
	_ context.Context,
	req adminRejectChangeRequest,
) (*adminRejectChangeResponse, error) {
	if err := sh.RejectChange(RejectChangeRequest(req)); err != nil {
		return nil, err
	}
	return &adminRejectChangeResponse{}, nil
}

type adminSetStatusBody struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

type adminSetStatusRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Number int    `path:"number" json:"-"`

	Name  string `json:"name"`
	State string `json:"state"`
}

type adminSetStatusResponse struct{}

// Check administration sets the latest named status for a change.
func (sh *ShamHub) handleAdminSetStatus(
	_ context.Context,
	req adminSetStatusRequest,
) (*adminSetStatusResponse, error) {
	state, err := parseChecksState(req.State)
	if err != nil {
		return nil, badRequestErrorf("%s", err)
	}
	if err := sh.SetChangeCheck(
		req.Owner,
		req.Repo,
		req.Number,
		forge.ChangeCheck{Name: req.Name, State: state},
	); err != nil {
		return nil, err
	}
	return &adminSetStatusResponse{}, nil
}

type adminSetMergeabilityBody struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type adminSetMergeabilityRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Number int    `path:"number" json:"-"`

	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type adminSetMergeabilityResponse struct{}

// Mergeability administration sets the merge gate state for a change.
func (sh *ShamHub) handleAdminSetMergeability(
	_ context.Context,
	req adminSetMergeabilityRequest,
) (*adminSetMergeabilityResponse, error) {
	mergeability, err := parseMergeability(req.State, req.Reason)
	if err != nil {
		return nil, badRequestErrorf("%s", err)
	}
	if err := sh.SetChangeMergeability(SetChangeMergeabilityRequest{
		Owner:        req.Owner,
		Repo:         req.Repo,
		Number:       req.Number,
		Mergeability: mergeability,
	}); err != nil {
		return nil, err
	}
	return &adminSetMergeabilityResponse{}, nil
}

type adminPostCommentBody struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`

	Change     int    `json:"change"`
	ID         int    `json:"id,omitempty"`
	Body       string `json:"body"`
	Resolvable bool   `json:"resolvable,omitempty"`
	Resolved   bool   `json:"resolved,omitempty"`
}

type adminPostCommentRequest struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`

	Change     int    `json:"change"`
	ID         int    `json:"id,omitempty"`
	Body       string `json:"body"`
	Resolvable bool   `json:"resolvable,omitempty"`
	Resolved   bool   `json:"resolved,omitempty"`
}

type adminPostCommentResponse struct {
	ID int `json:"id"`
}

// Comment creation supports explicit IDs for deterministic test fixtures.
func (sh *ShamHub) handleAdminPostComment(
	_ context.Context,
	req adminPostCommentRequest,
) (*adminPostCommentResponse, error) {
	id, err := sh.PostComment(PostCommentRequest(req))
	if err != nil {
		return nil, err
	}
	return &adminPostCommentResponse{ID: id}, nil
}

type adminEditCommentBody struct {
	Resolved *bool `json:"resolved,omitempty"`
}

type adminEditCommentRequest struct {
	ID int `path:"id" json:"-"`

	Resolved *bool `json:"resolved,omitempty"`
}

type adminEditCommentResponse struct{}

// Comment editing currently updates resolution state only.
func (sh *ShamHub) handleAdminEditComment(
	_ context.Context,
	req adminEditCommentRequest,
) (*adminEditCommentResponse, error) {
	if err := sh.EditComment(EditCommentRequest(req)); err != nil {
		return nil, err
	}
	return &adminEditCommentResponse{}, nil
}

type adminDeleteCommentRequest struct {
	ID int `path:"id" json:"-"`
}

type adminDeleteCommentResponse struct{}

// Comment deletion removes a seeded or forge-created comment by ID.
func (sh *ShamHub) handleAdminDeleteComment(
	_ context.Context,
	req adminDeleteCommentRequest,
) (*adminDeleteCommentResponse, error) {
	if err := sh.DeleteComment(req.ID); err != nil {
		return nil, err
	}
	return &adminDeleteCommentResponse{}, nil
}

type adminDumpChangesRequest struct{}

type adminDumpChangesResponse struct {
	Changes []*Change `json:"changes"`
}

// Change dumps return all change records for golden-file comparisons.
func (sh *ShamHub) handleAdminDumpChanges(
	_ context.Context,
	_ adminDumpChangesRequest,
) (*adminDumpChangesResponse, error) {
	changes, err := sh.ListChanges()
	if err != nil {
		return nil, err
	}
	return &adminDumpChangesResponse{Changes: changes}, nil
}

type adminDumpChangeRequest struct {
	Number int `path:"number" json:"-"`
}

type adminDumpChangeResponse struct {
	Change *Change `json:"change"`
}

// Single-change dumps return one change by number for CLI assertions.
func (sh *ShamHub) handleAdminDumpChange(
	_ context.Context,
	req adminDumpChangeRequest,
) (*adminDumpChangeResponse, error) {
	changes, err := sh.ListChanges()
	if err != nil {
		return nil, err
	}
	idx := slices.IndexFunc(changes, func(change *Change) bool {
		return change.Number == req.Number
	})
	if idx < 0 {
		return nil, notFoundErrorf("change %d not found", req.Number)
	}
	return &adminDumpChangeResponse{Change: changes[idx]}, nil
}

type adminDumpCommentsResponse struct {
	Comments []*ChangeComment `json:"comments"`
}

// Comment dumps keep repeated change query parameters for script ergonomics.
func (sh *ShamHub) handleAdminDumpComments(
	w http.ResponseWriter,
	r *http.Request,
) {
	comments, err := sh.ListChangeComments()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	changeIDs := make(map[int]struct{})
	for _, value := range r.URL.Query()["change"] {
		change, err := strconv.Atoi(value)
		if err != nil {
			http.Error(w, "invalid change query", http.StatusBadRequest)
			return
		}
		changeIDs[change] = struct{}{}
	}

	if len(changeIDs) > 0 {
		comments = slices.DeleteFunc(comments, func(comment *ChangeComment) bool {
			_, ok := changeIDs[comment.Change]
			return !ok
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(adminDumpCommentsResponse{
		Comments: comments,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

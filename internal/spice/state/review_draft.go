package state

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"

	"go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

const _reviewDraftsDir = "comments"

type reviewDraftState struct {
	NextID review.DraftID      `json:"nextID"`
	Drafts []storedReviewDraft `json:"comments"`
}

type storedReviewDraft struct {
	ID       review.DraftID `json:"id"`
	File     string         `json:"file"`
	Line     int            `json:"line"`
	Body     string         `json:"body"`
	ThreadID string         `json:"threadID,omitempty"`
}

// AddReviewDraft assigns a branch-local ID and saves a review draft.
func (s *Store) AddReviewDraft(
	ctx context.Context,
	branch string,
	draft review.Draft,
) (review.Draft, error) {
	state, err := s.loadReviewDraftState(ctx, branch)
	if err != nil {
		return review.Draft{}, err
	}
	if state == nil {
		state = &reviewDraftState{NextID: 1}
	}

	draft = draft.WithID(state.NextID)
	state.NextID++
	state.Drafts = append(state.Drafts, storeReviewDraft(draft))
	if err := s.saveReviewDraftState(ctx, branch, state); err != nil {
		return review.Draft{}, err
	}
	return draft, nil
}

// UpdateReviewDraftBody replaces the body of one branch-local draft.
func (s *Store) UpdateReviewDraftBody(
	ctx context.Context,
	branch string,
	id review.DraftID,
	body string,
) error {
	state, err := s.loadReviewDraftState(ctx, branch)
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("draft comment %d not found", id)
	}

	idx := slices.IndexFunc(state.Drafts, func(draft storedReviewDraft) bool {
		return draft.ID == id
	})
	if idx < 0 {
		return fmt.Errorf("draft comment %d not found", id)
	}
	state.Drafts[idx].Body = body
	return s.saveReviewDraftState(ctx, branch, state)
}

// LoadReviewDrafts retrieves the unpublished review comments for branch.
// It returns nil when the branch has no review drafts.
func (s *Store) LoadReviewDrafts(
	ctx context.Context,
	branch string,
) ([]review.Draft, error) {
	state, err := s.loadReviewDraftState(ctx, branch)
	if err != nil || state == nil {
		return nil, err
	}

	drafts := make([]review.Draft, len(state.Drafts))
	for i, stored := range state.Drafts {
		draft, err := loadReviewDraft(stored)
		if err != nil {
			return nil, fmt.Errorf("load draft %d: %w", stored.ID, err)
		}
		drafts[i] = draft
	}
	return drafts, nil
}

// ClearReviewDrafts removes review draft state for branch.
func (s *Store) ClearReviewDrafts(ctx context.Context, branch string) error {
	if err := s.db.Delete(
		ctx,
		reviewDraftsJSON(branch),
		fmt.Sprintf("%v: clear review drafts", branch),
	); err != nil {
		return fmt.Errorf("delete review drafts: %w", err)
	}
	return nil
}

func (s *Store) loadReviewDraftState(
	ctx context.Context,
	branch string,
) (*reviewDraftState, error) {
	var state reviewDraftState
	if err := s.db.Get(ctx, reviewDraftsJSON(branch), &state); err != nil {
		if errors.Is(err, storage.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("get review drafts: %w", err)
	}
	return &state, nil
}

func (s *Store) saveReviewDraftState(
	ctx context.Context,
	branch string,
	state *reviewDraftState,
) error {
	if err := s.db.Set(
		ctx,
		reviewDraftsJSON(branch),
		state,
		fmt.Sprintf("%v: save review drafts", branch),
	); err != nil {
		return fmt.Errorf("set review drafts: %w", err)
	}
	return nil
}

func storeReviewDraft(draft review.Draft) storedReviewDraft {
	stored := storedReviewDraft{
		ID:   draft.ID(),
		Body: draft.Body(),
	}
	if replyTo, ok := draft.ReplyTo(); ok {
		stored.ThreadID = replyTo
		return stored
	}

	anchor, _ := draft.Anchor()
	stored.File = anchor.Path()
	stored.Line = anchor.StartLine()
	return stored
}

func loadReviewDraft(stored storedReviewDraft) (review.Draft, error) {
	if stored.ThreadID != "" {
		return review.NewReplyDraft(
			stored.ID,
			stored.ThreadID,
			stored.Body,
		), nil
	}

	anchor, err := review.NewLineAnchor(stored.File, stored.Line)
	if err != nil {
		return review.Draft{}, fmt.Errorf("decode anchor: %w", err)
	}
	return review.NewCommentDraft(stored.ID, anchor, stored.Body), nil
}

func reviewDraftsJSON(branch string) string {
	return path.Join(_reviewDraftsDir, branch)
}

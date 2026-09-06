package state

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"

	"go.abhg.dev/gs/internal/jsonmut"
	"go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

const _reviewDraftsDir = "comments"

type reviewDraftState struct {
	LastID review.DraftID                       `json:"lastID"`
	Drafts map[review.DraftID]storedReviewDraft `json:"drafts"`
}

type storedReviewDraft struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Body     string `json:"body"`
	ThreadID string `json:"threadID,omitempty"`
}

// AddReviewDraft atomically assigns a branch-local ID and saves a draft.
func (s *Store) AddReviewDraft(
	ctx context.Context,
	branch string,
	draft review.Draft,
) (review.Draft, error) {
	stored, err := json.Marshal(storeReviewDraft(draft))
	if err != nil {
		return review.Draft{}, fmt.Errorf("encode review draft: %w", err)
	}
	id, err := storage.MutateJSON(
		ctx,
		s.db,
		storage.JSONMutationRequest{
			Key:       reviewDraftsJSON(branch),
			IfMissing: jsontext.Value(`{}`),
			Message:   fmt.Sprintf("%v: add review draft", branch),
		},
		jsonmut.InsertAutoIncrement(
			"/lastID",
			"/drafts",
			jsontext.Value(stored),
		),
	)
	if err != nil {
		return review.Draft{}, fmt.Errorf("add review draft: %w", err)
	}
	draft.ID = review.DraftID(id)
	return draft, nil
}

// UpdateReviewDraftBody atomically replaces one draft's body.
func (s *Store) UpdateReviewDraftBody(
	ctx context.Context,
	branch string,
	id review.DraftID,
	body string,
) error {
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode review draft body: %w", err)
	}
	err = storage.UpdateJSON(
		ctx,
		s.db,
		storage.JSONMutationRequest{
			Key:       reviewDraftsJSON(branch),
			IfMissing: jsontext.Value(`{}`),
			Message:   fmt.Sprintf("%v: update review draft", branch),
		},
		jsonmut.Replace(
			jsontext.Pointer("/drafts").
				AppendToken(id.String()).
				AppendToken("body"),
			jsontext.Value(bodyJSON),
		),
	)
	if errors.Is(err, jsonmut.ErrNotExist) {
		return fmt.Errorf("draft comment %d not found", id)
	}
	if err != nil {
		return fmt.Errorf("update review draft: %w", err)
	}
	return nil
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

	ids := slices.Sorted(maps.Keys(state.Drafts))
	drafts := make([]review.Draft, len(ids))
	for i, id := range ids {
		stored := state.Drafts[id]
		if stored.ThreadID != "" {
			drafts[i] = review.Draft{
				ID:      id,
				Body:    stored.Body,
				ReplyTo: stored.ThreadID,
			}
			continue
		}

		drafts[i] = review.Draft{
			ID:   id,
			Body: stored.Body,
			Anchor: review.Anchor{
				Path:      stored.File,
				StartLine: stored.Line,
				EndLine:   stored.Line,
			},
		}
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

func storeReviewDraft(draft review.Draft) storedReviewDraft {
	stored := storedReviewDraft{
		Body: draft.Body,
	}
	if draft.ReplyTo != "" {
		stored.ThreadID = draft.ReplyTo
		return stored
	}

	stored.File = draft.Anchor.Path
	stored.Line = draft.Anchor.StartLine
	return stored
}

func reviewDraftsJSON(branch string) string {
	return path.Join(_reviewDraftsDir, branch)
}

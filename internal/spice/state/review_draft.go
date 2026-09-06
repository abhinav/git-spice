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
	EndLine  int    `json:"endLine,omitempty"`
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
			Requires:  []string{branchKey(branch)},
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

// DeleteReviewDrafts atomically removes drafts by branch-local ID.
func (s *Store) DeleteReviewDrafts(
	ctx context.Context,
	branch string,
	ids []review.DraftID,
) (int, error) {
	requested := make(map[review.DraftID]struct{}, len(ids))
	for _, id := range ids {
		requested[id] = struct{}{}
	}

	statements := make([]jsonmut.Statement, 0, len(requested))
	for _, id := range slices.Sorted(maps.Keys(requested)) {
		path := jsontext.Pointer("/drafts").AppendToken(id.String())
		statements = append(statements,
			jsonmut.Lookup(path).Then(func(value jsontext.Value) jsonmut.Statement {
				if len(value) == 0 {
					return jsonmut.Fail[struct{}](
						&reviewDraftNotFoundError{ID: id},
					)
				}
				return jsonmut.Delete(path)
			}),
		)
	}

	err := storage.UpdateJSON(
		ctx,
		s.db,
		storage.JSONMutationRequest{
			Key:       reviewDraftsJSON(branch),
			IfMissing: jsontext.Value(`{}`),
			Requires:  []string{branchKey(branch)},
			Message:   fmt.Sprintf("%v: delete review drafts", branch),
		},
		jsonmut.Block(statements...),
	)
	if notFound, ok := errors.AsType[*reviewDraftNotFoundError](err); ok {
		return 0, notFound
	}
	if err != nil {
		return 0, fmt.Errorf("delete review drafts: %w", err)
	}
	return len(requested), nil
}

type reviewDraftNotFoundError struct {
	ID review.DraftID
}

func (e *reviewDraftNotFoundError) Error() string {
	return fmt.Sprintf("draft comment %d not found", e.ID)
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
			Requires:  []string{branchKey(branch)},
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

// RemovePublishedReviewDrafts removes unchanged drafts after publication.
func (s *Store) RemovePublishedReviewDrafts(
	ctx context.Context,
	branch string,
	published []review.Draft,
) error {
	statements := make([]jsonmut.Statement, 0, len(published))
	for _, draft := range published {
		stored := storeReviewDraft(draft)
		path := jsontext.Pointer("/drafts").AppendToken(draft.ID.String())
		statements = append(statements,
			jsonmut.Decode[*storedReviewDraft](path).Then(
				func(current *storedReviewDraft) jsonmut.Statement {
					if current == nil || *current != stored {
						return jsonmut.Block()
					}
					return jsonmut.Delete(path)
				},
			),
		)
	}

	// Forge submission happens before this mutation starts.
	// Replaying the program removes only values that still match the request,
	// leaving drafts added or edited while submission was in flight intact.
	err := storage.UpdateJSON(
		ctx,
		s.db,
		storage.JSONMutationRequest{
			Key:       reviewDraftsJSON(branch),
			IfMissing: jsontext.Value(`{}`),
			Requires:  []string{branchKey(branch)},
			Message:   fmt.Sprintf("%v: remove published review drafts", branch),
		},
		jsonmut.Block(statements...),
	)
	if errors.Is(err, storage.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove published review drafts: %w", err)
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

		endLine := stored.EndLine
		if stored.Line != 0 && endLine == 0 {
			endLine = stored.Line
		}
		drafts[i] = review.Draft{
			ID:   id,
			Body: stored.Body,
			Anchor: review.Anchor{
				Path:      stored.File,
				StartLine: stored.Line,
				EndLine:   endLine,
			},
		}
	}
	return drafts, nil
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
	stored.EndLine = draft.Anchor.EndLine
	return stored
}

func reviewDraftsJSON(branch string) string {
	return path.Join(_reviewDraftsDir, branch)
}

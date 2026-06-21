package restack

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/ui"
)

// QuestionPrompter collects answers from the user for a list of
// questions raised by the resolver.
//
// Implementations must return one answer per input question, in the
// same order. If the user aborts, return a non-nil error.
type QuestionPrompter interface {
	AskAnswers(ctx context.Context, questions []string) ([]string, error)
}

// ViewPrompter is a QuestionPrompter backed by a [ui.View].
//
// If the view is not interactive (e.g., gs is being piped),
// AskAnswers returns an error.
type ViewPrompter struct {
	View ui.View
}

var _ QuestionPrompter = (*ViewPrompter)(nil)

// NewViewPrompter wraps view as a QuestionPrompter.
func NewViewPrompter(view ui.View) *ViewPrompter {
	return &ViewPrompter{View: view}
}

// AskAnswers prompts for one answer per question.
//
// Questions are presented one at a time so each answer can inform
// the user's reading of the next prompt (questions may build on
// each other).
func (p *ViewPrompter) AskAnswers(
	_ context.Context, questions []string,
) ([]string, error) {
	if len(questions) == 0 {
		return nil, nil
	}
	if !ui.Interactive(p.View) {
		return nil, errors.New(
			"cannot prompt for answers: not running in interactive mode")
	}

	answers := make([]string, len(questions))
	for i, q := range questions {
		input := ui.NewInput().
			WithTitle(fmt.Sprintf("Question %d of %d", i+1, len(questions))).
			WithDescription(q).
			WithValue(&answers[i])

		if err := ui.Run(p.View, input); err != nil {
			return nil, fmt.Errorf("prompt: %w", err)
		}
	}
	return answers, nil
}

package scriptrun_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/scriptrun"
)

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  *scriptrun.ResolveResponse
	}{
		{
			name:  "Empty object",
			input: `{}`,
			want:  &scriptrun.ResolveResponse{},
		},
		{
			name:  "TitleOnly",
			input: `{"title": "Fix the thing"}`,
			want:  &scriptrun.ResolveResponse{Title: "Fix the thing"},
		},
		{
			name: "Full",
			input: `{
				"title": "T",
				"body": "B",
				"assumptions": ["a1"],
				"questions": ["q1"],
				"unresolved_files": ["f1"]
			}`,
			want: &scriptrun.ResolveResponse{
				Title:           "T",
				Body:            "B",
				Assumptions:     []string{"a1"},
				Questions:       []string{"q1"},
				UnresolvedFiles: []string{"f1"},
			},
		},
		{
			name:  "ExtraFieldsIgnored",
			input: `{"title": "T", "extra": 42}`,
			want:  &scriptrun.ResolveResponse{Title: "T"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := scriptrun.ParseResponse([]byte(tt.input))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseResponse_emptyInput(t *testing.T) {
	_, err := scriptrun.ParseResponse(nil)
	assert.ErrorIs(t, err, scriptrun.ErrEmptyOutput)
}

func TestParseResponse_invalidJSON(t *testing.T) {
	_, err := scriptrun.ParseResponse([]byte("not json"))
	var invalid *scriptrun.InvalidOutputError
	require.ErrorAs(t, err, &invalid)
	assert.NotEmpty(t, invalid.Output)
	assert.Contains(t, invalid.Error(), "invalid script output")
}

func TestInvalidOutputError_unwraps(t *testing.T) {
	inner := errors.New("boom")
	e := &scriptrun.InvalidOutputError{Err: inner}
	assert.ErrorIs(t, e, inner)
}

package jsonmut_test

import (
	"encoding/json/jsontext"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/jsonmut"
)

func TestApply_invalidDocument(t *testing.T) {
	t.Parallel()

	updated, value, err := jsonmut.Apply(
		jsontext.Value(`{`),
		jsonmut.Lookup("/answer"),
	)
	require.Error(t, err)
	assert.Nil(t, updated)
	assert.Empty(t, value)
}

func TestBlock(t *testing.T) {
	t.Parallel()

	document := jsontext.Value(`{
		"first": "before",
		"second": "before"
	}`)
	updated, _, err := jsonmut.Apply(
		document,
		jsonmut.Block(
			jsonmut.Replace("/first", jsontext.Value(`"after"`)),
			jsonmut.Set("/third", jsontext.Value(`3`)),
		),
	)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"first": "after",
		"second": "before",
		"third": 3
	}`, updated.String())
}

func TestBlock_StopsAtError(t *testing.T) {
	t.Parallel()

	document := jsontext.Value(`{"first":"before"}`)
	original := document.Clone()
	updated, _, err := jsonmut.Apply(
		document,
		jsonmut.Block(
			jsonmut.Set("/added", jsontext.Value(`true`)),
			jsonmut.Replace("/missing", jsontext.Value(`null`)),
		),
	)
	assert.ErrorIs(t, err, jsonmut.ErrNotExist)
	assert.Nil(t, updated)
	assert.Equal(t, original, document)
}

func TestProgram_Then(t *testing.T) {
	t.Parallel()

	updated, result, err := jsonmut.Apply(
		jsontext.Value(`{"source":41}`),
		jsonmut.Decode[int64]("/source").Then(
			func(value int64) jsonmut.Program[int64] {
				value++
				return jsonmut.Set(
					"/destination",
					jsontext.Value(strconv.FormatInt(value, 10)),
				).Returning(value)
			},
		),
	)
	require.NoError(t, err)
	assert.Equal(t, int64(42), result)
	assert.JSONEq(t, `{"source":41,"destination":42}`, updated.String())
}

func TestProgram_Then_stopsAfterError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("great sadness")
	var continued bool
	updated, result, err := jsonmut.Apply(
		jsontext.Value(`{}`),
		jsonmut.Fail[int](wantErr).Then(
			func(int) jsonmut.Program[string] {
				continued = true
				return jsonmut.Fail[string](errors.New("continuation called"))
			},
		),
	)
	assert.ErrorIs(t, err, wantErr)
	assert.Nil(t, updated)
	assert.Empty(t, result)
	assert.False(t, continued)
}

func TestProgram_Returning_preservesError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("great sadness")
	updated, result, err := jsonmut.Apply(
		jsontext.Value(`{}`),
		jsonmut.Fail[int](wantErr).Returning("unexpected result"),
	)
	assert.ErrorIs(t, err, wantErr)
	assert.Nil(t, updated)
	assert.Empty(t, result)
}

func TestLookup(t *testing.T) {
	t.Parallel()

	t.Run("Present", func(t *testing.T) {
		updated, value, err := jsonmut.Apply(
			jsontext.Value(`{"answer":42}`),
			jsonmut.Lookup("/answer"),
		)
		require.NoError(t, err)
		assert.Equal(t, jsontext.Value(`42`), value)
		assert.JSONEq(t, `{"answer":42}`, updated.String())
	})

	t.Run("Missing", func(t *testing.T) {
		_, value, err := jsonmut.Apply(
			jsontext.Value(`{}`),
			jsonmut.Lookup("/answer"),
		)
		require.NoError(t, err)
		assert.Len(t, value, 0)
	})
}

func TestLookup_arrayElement(t *testing.T) {
	t.Parallel()

	updated, value, err := jsonmut.Apply(
		jsontext.Value(`{"items":[{"name":"first"}]}`),
		jsonmut.Lookup("/items/0/name"),
	)
	require.Error(t, err)
	assert.Nil(t, updated)
	assert.Empty(t, value)
}

func TestDecode(t *testing.T) {
	t.Parallel()

	t.Run("Present", func(t *testing.T) {
		_, value, err := jsonmut.Apply(
			jsontext.Value(`{"answer":42}`),
			jsonmut.Decode[int64]("/answer"),
		)
		require.NoError(t, err)
		assert.Equal(t, int64(42), value)
	})

	t.Run("Missing", func(t *testing.T) {
		_, value, err := jsonmut.Apply(
			jsontext.Value(`{}`),
			jsonmut.Decode[int64]("/answer"),
		)
		require.NoError(t, err)
		assert.Zero(t, value)
	})

	t.Run("MissingPointer", func(t *testing.T) {
		_, value, err := jsonmut.Apply(
			jsontext.Value(`{}`),
			jsonmut.Decode[*int64]("/answer"),
		)
		require.NoError(t, err)
		assert.Nil(t, value)
	})

	t.Run("PresentPointer", func(t *testing.T) {
		_, value, err := jsonmut.Apply(
			jsontext.Value(`{"answer":0}`),
			jsonmut.Decode[*int64]("/answer"),
		)
		require.NoError(t, err)
		if assert.NotNil(t, value) {
			assert.Zero(t, *value)
		}
	})

	t.Run("WrongType", func(t *testing.T) {
		_, _, err := jsonmut.Apply(
			jsontext.Value(`{"answer":"forty-two"}`),
			jsonmut.Decode[int64]("/answer"),
		)
		assert.Error(t, err)
	})
}

func TestSet_replayDoesNotAliasReplacement(t *testing.T) {
	t.Parallel()

	replacement := jsontext.Value(`{"value":"before"}`)
	program := jsonmut.Set("", replacement)

	copy(replacement, jsontext.Value(`{"value":"mutate"}`))
	first, _, err := jsonmut.Apply(jsontext.Value(`{}`), program)
	require.NoError(t, err)
	assert.JSONEq(t, `{"value":"before"}`, first.String())

	copy(first, jsontext.Value(`{"value":"second"}`))
	second, _, err := jsonmut.Apply(jsontext.Value(`{}`), program)
	require.NoError(t, err)
	assert.JSONEq(t, `{"value":"before"}`, second.String())
}

func TestSet_arrayElement(t *testing.T) {
	t.Parallel()

	updated, _, err := jsonmut.Apply(
		jsontext.Value(`{"items":[{"name":"first"}]}`),
		jsonmut.Set("/items/0/name", jsontext.Value(`"updated"`)),
	)
	require.Error(t, err)
	assert.Nil(t, updated)
}

func TestIncrement_overflow(t *testing.T) {
	t.Parallel()

	document := jsontext.Value(`{"counter":9223372036854775807}`)
	original := document.Clone()
	updated, result, err := jsonmut.Apply(
		document,
		jsonmut.Increment("/counter"),
	)
	require.Error(t, err)
	assert.Nil(t, updated)
	assert.Zero(t, result)
	assert.Equal(t, original, document)
}

func TestInsertAutoIncrement(t *testing.T) {
	t.Parallel()

	document := jsontext.Value(`{
		"unrelated": {"preserved": true},
		"lastID": 41,
		"drafts": {"9": {"body": "existing"}}
	}`)
	original := document.Clone()

	updated, id, err := jsonmut.Apply(
		document,
		jsonmut.InsertAutoIncrement(
			"/lastID",
			"/drafts",
			jsontext.Value(`{"body":"new"}`),
		),
	)
	require.NoError(t, err)
	assert.Equal(t, int64(42), id)
	assert.JSONEq(t, `{
		"unrelated": {"preserved": true},
		"lastID": 42,
		"drafts": {
			"9": {"body": "existing"},
			"42": {"body": "new"}
		}
	}`, updated.String())
	assert.Equal(t, original, document, "Apply must not mutate its input")
}

func TestInsertAutoIncrement_InitializesMissingMembers(t *testing.T) {
	t.Parallel()

	updated, id, err := jsonmut.Apply(
		jsontext.Value(`{}`),
		jsonmut.InsertAutoIncrement(
			"/lastID",
			"/drafts",
			jsontext.Value(`{"body":"first"}`),
		),
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), id)
	assert.JSONEq(t, `{
		"lastID": 1,
		"drafts": {"1": {"body": "first"}}
	}`, updated.String())
}

func TestInsertAutoIncrement_existingNextMember(t *testing.T) {
	t.Parallel()

	document := jsontext.Value(`{
		"lastID": 41,
		"drafts": {"42": {"body": "existing"}}
	}`)
	original := document.Clone()
	updated, id, err := jsonmut.Apply(
		document,
		jsonmut.InsertAutoIncrement(
			"/lastID",
			"/drafts",
			jsontext.Value(`{"body":"new"}`),
		),
	)
	assert.ErrorIs(t, err, jsonmut.ErrExist)
	assert.Nil(t, updated)
	assert.Zero(t, id)
	assert.Equal(t, original, document)
}

func TestReplace(t *testing.T) {
	t.Parallel()

	updated, _, err := jsonmut.Apply(
		jsontext.Value(`{
			"drafts": {"7": {"body": "before", "file": "main.go"}}
		}`),
		jsonmut.Replace(
			jsontext.Pointer("/drafts").
				AppendToken("7").
				AppendToken("body"),
			jsontext.Value(`"after"`),
		),
	)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"drafts": {"7": {"body": "after", "file": "main.go"}}
	}`, updated.String())
}

func TestReplace_Missing(t *testing.T) {
	t.Parallel()

	_, _, err := jsonmut.Apply(
		jsontext.Value(`{"drafts": {}}`),
		jsonmut.Replace(
			"/drafts/7/body",
			jsontext.Value(`"after"`),
		),
	)
	assert.ErrorIs(t, err, jsonmut.ErrNotExist)
}

func TestSet_missingParent(t *testing.T) {
	t.Parallel()

	_, _, err := jsonmut.Apply(
		jsontext.Value(`{}`),
		jsonmut.Set(
			"/drafts/7/body",
			jsontext.Value(`"after"`),
		),
	)
	assert.ErrorIs(t, err, jsonmut.ErrNotExist)
}

func TestInsert_Existing(t *testing.T) {
	t.Parallel()

	_, _, err := jsonmut.Apply(
		jsontext.Value(`{"answer":42}`),
		jsonmut.Insert("/answer", jsontext.Value(`43`)),
	)
	assert.ErrorIs(t, err, jsonmut.ErrExist)
}

func TestFail(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("great sadness")
	updated, _, err := jsonmut.Apply(
		jsontext.Value(`{}`),
		jsonmut.Fail[int64](wantErr),
	)
	assert.ErrorIs(t, err, wantErr)
	assert.Nil(t, updated)
}

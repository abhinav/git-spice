package jsonmut_test

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/jsonmut"
	"pgregory.net/rapid"
)

const maxGeneratedJSONDepth = 10

// TestLookup_matchesReference compares lookup behavior
// with an independent decoded-object model.
func TestLookup_matchesReference(t *testing.T) {
	rapid.Check(t, testLookupMatchesReference)
}

// FuzzLookup_matchesReference exposes the lookup property to Go fuzzing.
func FuzzLookup_matchesReference(f *testing.F) {
	f.Fuzz(rapid.MakeFuzz(testLookupMatchesReference))
}

func testLookupMatchesReference(t *rapid.T) {
	document := jsonObjectGenerator(maxGeneratedJSONDepth).Draw(t, "document")
	path := drawJSONPath(t, document)
	documentJSON := marshalJSON(t, document)
	original := documentJSON.Clone()

	updated, got, err := jsonmut.Apply(
		documentJSON,
		jsonmut.Lookup(jsonPointer(path)),
	)
	want, found, wantErr := referenceLookup(document, path)
	if wantErr != referenceWrongType {
		require.NoError(t, err)
		if found {
			assert.JSONEq(t, marshalJSON(t, want).String(), got.String())
		} else {
			assert.Empty(t, got)
		}
		assert.JSONEq(t, original.String(), updated.String())
	} else {
		require.Error(t, err)
		assert.Nil(t, updated)
	}
	assert.Equal(t, original, documentJSON, "Apply must not mutate its input")
}

// TestMutation_matchesReference compares every mutation mode
// with an independent decoded-object model.
func TestMutation_matchesReference(t *testing.T) {
	rapid.Check(t, testMutationMatchesReference)
}

// FuzzMutation_matchesReference exposes the mutation property to Go fuzzing.
func FuzzMutation_matchesReference(f *testing.F) {
	f.Fuzz(rapid.MakeFuzz(testMutationMatchesReference))
}

func testMutationMatchesReference(t *rapid.T) {
	document := jsonObjectGenerator(maxGeneratedJSONDepth).Draw(t, "document")
	path := drawJSONPath(t, document)
	replacement := jsonValueGenerator(maxGeneratedJSONDepth).
		Draw(t, "replacement")
	mode := rapid.SampledFrom([]referenceMutation{
		referenceSet,
		referenceSetIfAbsent,
		referenceInsert,
		referenceReplace,
	}).Draw(t, "mode")

	documentJSON := marshalJSON(t, document)
	original := documentJSON.Clone()
	updated, _, err := jsonmut.Apply(
		documentJSON,
		mode.statement(jsonPointer(path), marshalJSON(t, replacement)),
	)
	want, wantErr := referenceMutate(document, path, replacement, mode)

	switch wantErr {
	case referenceNoError:
		require.NoError(t, err)
		require.True(t, updated.IsValid(), "mutation must produce valid JSON")
		assert.JSONEq(t, marshalJSON(t, want).String(), updated.String())
	case referenceAlreadyExists:
		assert.ErrorIs(t, err, jsonmut.ErrExist)
		assert.Nil(t, updated)
	case referenceDoesNotExist:
		assert.ErrorIs(t, err, jsonmut.ErrNotExist)
		assert.Nil(t, updated)
	case referenceWrongType:
		require.Error(t, err)
		assert.False(t, errors.Is(err, jsonmut.ErrExist))
		assert.False(t, errors.Is(err, jsonmut.ErrNotExist))
		assert.Nil(t, updated)
	default:
		t.Fatalf("unknown reference error: %v", wantErr)
	}
	assert.Equal(t, original, documentJSON, "Apply must not mutate its input")
}

// referenceMutation selects equivalent operations
// in jsonmut and the decoded reference model.
//
// The reference model mutates decoded Go values rather than raw JSON.
// This gives the streaming implementation an independent semantic oracle
// without making whitespace or object-member order part of the contract.
type referenceMutation uint8

const (
	referenceSet referenceMutation = iota
	referenceSetIfAbsent
	referenceInsert
	referenceReplace
)

func (m referenceMutation) statement(
	path jsontext.Pointer,
	value jsontext.Value,
) jsonmut.Statement {
	switch m {
	case referenceSet:
		return jsonmut.Set(path, value)
	case referenceSetIfAbsent:
		return jsonmut.SetIfAbsent(path, value)
	case referenceInsert:
		return jsonmut.Insert(path, value)
	case referenceReplace:
		return jsonmut.Replace(path, value)
	default:
		panic("unknown reference mutation")
	}
}

// referenceError records observable failure classes
// without sharing jsonmut's error values with the reference model.
type referenceError uint8

const (
	referenceNoError referenceError = iota
	referenceAlreadyExists
	referenceDoesNotExist
	referenceWrongType
)

// referenceLookup follows path through decoded JSON objects.
// A missing member is a successful lookup with no value;
// traversing through a non-object is an invalid operation instead.
func referenceLookup(
	value any,
	path []string,
) (_ any, found bool, _ referenceError) {
	for _, member := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false, referenceWrongType
		}
		value, ok = object[member]
		if !ok {
			return nil, false, referenceNoError
		}
	}
	return value, true, referenceNoError
}

// referenceMutate applies mode to decoded JSON values in place.
// Production instead returns transformed raw JSON,
// so agreement exercises behavior without sharing implementation.
func referenceMutate(
	document any,
	path []string,
	replacement any,
	mode referenceMutation,
) (any, referenceError) {
	if len(path) == 0 {
		switch mode {
		case referenceInsert:
			return nil, referenceAlreadyExists
		case referenceSetIfAbsent:
			return document, referenceNoError
		default:
			return replacement, referenceNoError
		}
	}

	object, ok := document.(map[string]any)
	if !ok {
		return nil, referenceWrongType
	}
	member := path[0]
	current, exists := object[member]
	if len(path) == 1 {
		switch {
		case mode == referenceInsert && exists:
			return nil, referenceAlreadyExists
		case mode == referenceReplace && !exists:
			return nil, referenceDoesNotExist
		case mode == referenceSetIfAbsent && exists:
			return document, referenceNoError
		default:
			object[member] = replacement
			return document, referenceNoError
		}
	}
	if !exists {
		return nil, referenceDoesNotExist
	}

	current, err := referenceMutate(current, path[1:], replacement, mode)
	if err != referenceNoError {
		return nil, err
	}
	object[member] = current
	return document, referenceNoError
}

// jsonLocation associates a path with the value found there.
// Locations include the root and values of every JSON kind.
type jsonLocation struct {
	Path  []string
	Value any
}

// drawJSONPath chooses among paths known to exist,
// paths known to be missing, and arbitrary paths.
func drawJSONPath(t *rapid.T, document map[string]any) []string {
	locations := collectJSONLocations(document, nil)
	switch rapid.IntRange(0, 2).Draw(t, "pathKind") {
	case 0:
		// Existing paths exercise successful traversal to roots,
		// objects, arrays, and scalar values.
		location := rapid.SampledFrom(locations).Draw(t, "existingPath")
		return slices.Clone(location.Path)
	case 1:
		// Build from an existing object to guarantee a missing member.
		// An optional descendant turns that member into a missing parent.
		var objects []jsonLocation
		for _, location := range locations {
			if _, ok := location.Value.(map[string]any); ok {
				objects = append(objects, location)
			}
		}
		parent := rapid.SampledFrom(objects).Draw(t, "parentPath")
		object := parent.Value.(map[string]any)
		member := jsonMemberGenerator().
			Filter(func(member string) bool {
				_, exists := object[member]
				return !exists
			}).
			Draw(t, "missingMember")
		path := append(slices.Clone(parent.Path), member)
		if rapid.Bool().Draw(t, "missingParent") {
			path = append(path, jsonMemberGenerator().Draw(t, "descendant"))
		}
		return path
	default:
		// Independent paths cover chance intersections and attempts
		// to traverse through non-object values.
		return rapid.SliceOfN(
			jsonMemberGenerator(),
			0,
			maxGeneratedJSONDepth+2,
		).
			Draw(t, "arbitraryPath")
	}
}

// collectJSONLocations records the root and every descendant value.
func collectJSONLocations(value any, path []string) []jsonLocation {
	locations := []jsonLocation{{Path: slices.Clone(path), Value: value}}
	object, ok := value.(map[string]any)
	if !ok {
		return locations
	}
	// Stable ordering makes a Rapid choice reproducible while shrinking
	// even though Go map iteration order is not stable.
	for _, member := range slices.Sorted(maps.Keys(object)) {
		childPath := append(slices.Clone(path), member)
		locations = append(
			locations,
			collectJSONLocations(object[member], childPath)...,
		)
	}
	return locations
}

// jsonObjectGenerator generates an object with values nested up to depth.
func jsonObjectGenerator(depth int) *rapid.Generator[map[string]any] {
	return rapid.MapOfN(jsonMemberGenerator(), jsonValueGenerator(depth), 0, 4)
}

// jsonValueGenerator generates values nested up to depth.
func jsonValueGenerator(depth int) *rapid.Generator[any] {
	options := []*rapid.Generator[any]{
		rapid.Just[any](nil),
		rapid.Bool().AsAny(),
		rapid.Int64Range(-10_000, 10_000).AsAny(),
		rapid.StringN(0, 8, 32).AsAny(),
	}
	// Bounded recursion adds nested objects and arrays
	// without allowing generated documents to grow without limit.
	if depth > 0 {
		options = append(
			options,
			jsonObjectGenerator(depth-1).AsAny(),
			rapid.SliceOfN(jsonValueGenerator(depth-1), 0, 4).AsAny(),
		)
	}
	return rapid.OneOf(options...)
}

func jsonMemberGenerator() *rapid.Generator[string] {
	// Concentrate generated names on JSON Pointer escapes,
	// JSON string escapes, whitespace, Unicode, and ordinary characters.
	return rapid.StringOfN(rapid.RuneFrom([]rune("ab09/~\" \n\u2603")), 0, 6, 24)
}

func jsonPointer(path []string) jsontext.Pointer {
	var pointer jsontext.Pointer
	for _, member := range path {
		pointer = pointer.AppendToken(member)
	}
	return pointer
}

func marshalJSON(t rapid.TB, value any) jsontext.Value {
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return jsontext.Value(encoded)
}

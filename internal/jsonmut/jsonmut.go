// Package jsonmut defines composable, typed transformations over JSON objects.
// A [Program] transforms one document and returns a typed result,
// while a [Statement] is a program whose result is not meaningful.
// Applying either leaves the input document unchanged,
// so callers may replay the same transformation against a newer document.
//
// Use [Block] when statements are independent but must run in order:
//
//	program := jsonmut.Block(
//		jsonmut.Set("/status", jsontext.Value(`"ready"`)),
//		jsonmut.Insert("/items/42", itemJSON),
//	)
//
// Use [Program.Then] when a later transformation depends on an earlier result:
//
//	program := jsonmut.Increment("/lastID").Then(
//		func(id int64) jsonmut.Program[int64] {
//			path := jsontext.Pointer("/items").AppendToken(strconv.FormatInt(id, 10))
//			return jsonmut.Insert(path, itemJSON).Returning(id)
//		},
//	)
//
// Because a program may be replayed,
// continuations passed to [Program.Then] must depend only on their argument
// and immutable captured inputs.
//
// # Limitations
//
// Paths address object members only;
// they cannot traverse or rewrite array elements.
package jsonmut

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"math"
	"slices"
	"strconv"

	"go.abhg.dev/gs/internal/must"
)

var (
	// ErrExist indicates that a statement expected a JSON member to be absent.
	ErrExist = errors.New("JSON value already exists")

	// ErrNotExist indicates that a statement expected a JSON member to exist.
	ErrNotExist = errors.New("JSON value does not exist")
)

// Program is a replayable JSON transformation that returns a value of type T.
// The zero value is not a valid program.
type Program[T any] struct {
	apply func(jsontext.Value) (jsontext.Value, T, error)
}

// Statement is a [Program] whose result is not meaningful.
type Statement = Program[struct{}]

// Apply runs program against document without modifying document.
// It returns no updated document or result when the program fails.
func Apply[T any](
	document jsontext.Value,
	program Program[T],
) (jsontext.Value, T, error) {
	var zero T
	if !document.IsValid() {
		return nil, zero, errors.New("invalid JSON document")
	}
	must.Bef(program.apply != nil, "cannot apply a zero Program")
	return program.apply(document.Clone())
}

// Then runs the program returned by next against the updated document.
//
// Reapplying the returned program calls next again,
// possibly with a different value.
// next must not mutate value and must depend only on value
// and immutable captured inputs.
func (p Program[A]) Then[B any](
	next func(A) Program[B],
) Program[B] {
	must.Bef(p.apply != nil, "cannot compose a zero Program")
	must.Bef(next != nil, "Program.Then requires a continuation")
	return Program[B]{
		apply: func(document jsontext.Value) (jsontext.Value, B, error) {
			var zero B
			document, value, err := p.apply(document)
			if err != nil {
				return nil, zero, err
			}

			program := next(value)
			must.Bef(
				program.apply != nil,
				"Program.Then continuation returned a zero Program",
			)
			return program.apply(document)
		},
	}
}

// Returning runs p and replaces its result with value.
// value must remain immutable while the returned program may be applied.
func (p Program[A]) Returning[B any](value B) Program[B] {
	must.Bef(p.apply != nil, "cannot compose a zero Program")
	return Program[B]{
		apply: func(document jsontext.Value) (jsontext.Value, B, error) {
			document, _, err := p.apply(document)
			if err != nil {
				var zero B
				return nil, zero, err
			}
			return document, value, nil
		},
	}
}

// Block returns a statement that runs statements in order.
func Block(statements ...Statement) Statement {
	for i, statement := range statements {
		must.Bef(statement.apply != nil, "statement %d is a zero Statement", i)
	}
	// A program may outlive the slice used to construct it.
	// Keep its replay sequence independent of later caller mutations.
	statements = slices.Clone(statements)
	return Statement{
		apply: func(document jsontext.Value) (jsontext.Value, struct{}, error) {
			for _, statement := range statements {
				var err error
				document, _, err = statement.apply(document)
				if err != nil {
					return nil, struct{}{}, err
				}
			}
			return document, struct{}{}, nil
		},
	}
}

// Lookup returns the JSON value at path without changing the document.
// It returns an empty value when path does not exist.
func Lookup(path jsontext.Pointer) Program[jsontext.Value] {
	must.Bef(path.IsValid(), "invalid JSON pointer %q", path)
	return Program[jsontext.Value]{
		apply: func(document jsontext.Value) (
			jsontext.Value,
			jsontext.Value,
			error,
		) {
			value, ok, err := lookup(document, path)
			if err != nil {
				return nil, nil, err
			}
			if !ok {
				return document, nil, nil
			}
			return document, value, nil
		},
	}
}

// Decode returns the value at path decoded as T.
// It returns the zero value of T when path does not exist.
// Decode a pointer type to distinguish an absent value from a present,
// non-null zero value; absence and JSON null both decode to nil.
func Decode[T any](path jsontext.Pointer) Program[T] {
	return Lookup(path).Then(func(value jsontext.Value) Program[T] {
		var decoded T
		if len(value) == 0 {
			return returnValue(decoded)
		}
		if err := json.Unmarshal(value, &decoded); err != nil {
			return Fail[T](fmt.Errorf("decode %q: %w", path, err))
		}
		return returnValue(decoded)
	})
}

// Set returns a statement that adds or replaces the value at path.
// Parent objects addressed by path must exist.
func Set(path jsontext.Pointer, value jsontext.Value) Statement {
	return newMutationStatement(path, value, mutationUpsert)
}

// SetIfAbsent returns a statement that adds value only when path is absent.
// Parent objects addressed by path must exist.
func SetIfAbsent(path jsontext.Pointer, value jsontext.Value) Statement {
	return newMutationStatement(path, value, mutationIfAbsent)
}

// Insert returns a statement that adds value at path.
// It returns [ErrExist] when path already exists.
// Parent objects addressed by path must exist.
func Insert(path jsontext.Pointer, value jsontext.Value) Statement {
	return newMutationStatement(path, value, mutationInsert)
}

// Replace returns a statement that replaces the value at path.
// It returns [ErrNotExist] when path does not exist.
func Replace(path jsontext.Pointer, value jsontext.Value) Statement {
	return newMutationStatement(path, value, mutationReplace)
}

// Delete returns a statement that removes the object member at path.
// It returns [ErrNotExist] when path does not exist.
func Delete(path jsontext.Pointer) Statement {
	must.Bef(path.IsValid(), "invalid JSON pointer %q", path)
	must.Bef(path != "", "delete path must not be the document root")
	return newMutationStatement(path, nil, mutationDelete)
}

type mutationMode uint8

const (
	mutationUpsert mutationMode = iota
	mutationIfAbsent
	mutationInsert
	mutationReplace
	mutationDelete
)

func newMutationStatement(
	path jsontext.Pointer,
	value jsontext.Value,
	mode mutationMode,
) Statement {
	must.Bef(path.IsValid(), "invalid JSON pointer %q", path)
	if mode != mutationDelete {
		must.Bef(value.IsValid(), "invalid JSON mutation value")
		// Programs may run after the caller reuses value's backing buffer.
		// Capture immutable input so every replay applies the same mutation.
		value = value.Clone()
	}
	return Statement{
		apply: func(document jsontext.Value) (jsontext.Value, struct{}, error) {
			updated, err := applyMutation(document, path, value, mode)
			return updated, struct{}{}, err
		},
	}
}

func applyMutation(
	document jsontext.Value,
	pointer jsontext.Pointer,
	replacement jsontext.Value,
	mode mutationMode,
) (jsontext.Value, error) {
	operation := "set"
	switch mode {
	case mutationIfAbsent:
		operation = "set if absent"
	case mutationInsert:
		operation = "insert"
	case mutationReplace:
		operation = "replace"
	case mutationDelete:
		operation = "delete"
	}

	next, stop := iter.Pull(pointer.Tokens())
	defer stop()

	updated, _, err := rewriteAtPath(
		document,
		next,
		replacement,
		mode,
	)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", operation, pointer, err)
	}
	return updated, nil
}

// rewriteAtPath consumes next to select and rewrite a value in document.
// It returns changed false only when setIfAbsent finds an existing target.
//
// The traversal retains one frame for each ancestor object.
// Each frame holds the encoded object prefix and a decoder positioned after
// the selected child, allowing the function to rebuild ancestors inside out.
func rewriteAtPath(
	document jsontext.Value,
	next func() (string, bool),
	replacement jsontext.Value,
	mode mutationMode,
) (updated jsontext.Value, changed bool, _ error) {
	member, ok := next()
	if !ok {
		// An empty pointer selects the complete document,
		// so no object traversal or ancestor reconstruction is necessary.
		switch mode {
		case mutationInsert:
			return nil, false, ErrExist
		case mutationIfAbsent:
			return document, false, nil
		default:
			return replacement.Clone(), true, nil
		}
	}

	// SetIfAbsent returns the complete input when the target already exists,
	// even though traversal may have started encoding object prefixes.
	original := document
	var ancestors []*objectRewriteFrame

	// Traverse only the ancestor members. Looking ahead one token distinguishes
	// an ancestor from the final target without resolving the target itself.
	// Each retained frame has copied the prefix before its selected child and
	// leaves its decoder positioned at the suffix needed during unwinding.
	for {
		nextMember, ok := next()
		if !ok {
			break
		}

		frame, err := newObjectRewriteFrame(document)
		if err != nil {
			return nil, false, err
		}
		found, err := frame.advanceTo(member)
		if err != nil {
			return nil, false, err
		}

		if !found {
			return nil, false, ErrNotExist
		}
		// advanceTo leaves the selected name unwritten.
		// This member is an ancestor, so preserve its name now.
		// Unwinding supplies its transformed child value.
		if err := frame.encoder.WriteToken(jsontext.String(member)); err != nil {
			return nil, false, fmt.Errorf("write member %q: %w", member, err)
		}

		child, err := frame.decoder.ReadValue()
		if err != nil {
			return nil, false, fmt.Errorf("read member %q: %w", member, err)
		}
		ancestors = append(ancestors, frame)
		document = child
		member = nextMember
	}

	// Resolve the final target only after traversal has established its parent.
	frame, err := newObjectRewriteFrame(document)
	if err != nil {
		return nil, false, err
	}
	found, err := frame.advanceTo(member)
	if err != nil {
		return nil, false, err
	}
	if !found {
		if mode == mutationReplace || mode == mutationDelete {
			return nil, false, ErrNotExist
		}
		if err := frame.encoder.WriteToken(jsontext.String(member)); err != nil {
			return nil, false, fmt.Errorf("write member %q: %w", member, err)
		}
		if err := frame.encoder.WriteValue(replacement); err != nil {
			return nil, false, fmt.Errorf("write member %q: %w", member, err)
		}
	} else {
		switch mode {
		case mutationInsert:
			return nil, false, ErrExist
		case mutationIfAbsent:
			return original, false, nil
		}
		// advanceTo left the selected name unwritten.
		// Replacement modes emit the name and new value;
		// deletion emits neither part of the member.
		if err := frame.decoder.SkipValue(); err != nil {
			return nil, false, fmt.Errorf("skip member %q: %w", member, err)
		}
		if mode != mutationDelete {
			if err := frame.encoder.WriteToken(jsontext.String(member)); err != nil {
				return nil, false, fmt.Errorf("write member %q: %w", member, err)
			}
			if err := frame.encoder.WriteValue(replacement); err != nil {
				return nil, false, fmt.Errorf("write member %q: %w", member, err)
			}
		}
	}
	updated, err = frame.finish()
	if err != nil {
		return nil, false, err
	}

	// Rebuild ancestors from the target outward. Writing the transformed child
	// resumes each parent frame before finish copies its untouched suffix.
	for _, frame := range slices.Backward(ancestors) {
		if err := frame.encoder.WriteValue(updated); err != nil {
			return nil, false, fmt.Errorf("write child object: %w", err)
		}
		var err error
		updated, err = frame.finish()
		if err != nil {
			return nil, false, err
		}
	}
	return updated, true, nil
}

// objectRewriteFrame retains an object traversal paused at one selected member.
// output contains the opening delimiter and every member before the selection.
type objectRewriteFrame struct {
	decoder *jsontext.Decoder
	output  *bytes.Buffer
	encoder *jsontext.Encoder
}

func newObjectRewriteFrame(document jsontext.Value) (*objectRewriteFrame, error) {
	if document.Kind() != '{' {
		return nil, fmt.Errorf(
			"expected JSON object, got %v",
			document.Kind(),
		)
	}

	output := new(bytes.Buffer)
	frame := &objectRewriteFrame{
		decoder: jsontext.NewDecoder(bytes.NewReader(document)),
		output:  output,
		encoder: jsontext.NewEncoder(output),
	}
	begin, err := frame.decoder.ReadToken()
	if err != nil {
		return nil, fmt.Errorf("read object opening: %w", err)
	}
	if err := frame.encoder.WriteToken(begin); err != nil {
		return nil, fmt.Errorf("write object opening: %w", err)
	}
	return frame, nil
}

// advanceTo copies complete object members until member is found.
// When it succeeds, it leaves both the member name and value unwritten,
// with the decoder positioned before the value.
func (f *objectRewriteFrame) advanceTo(member string) (bool, error) {
	for f.decoder.PeekKind() != '}' {
		name, err := f.decoder.ReadToken()
		if err != nil {
			return false, fmt.Errorf("read member name: %w", err)
		}
		nameString := name.String()
		if nameString == member {
			// Leave the selected name out of the encoded prefix.
			// The caller decides whether to preserve it with a transformed value
			// or omit the complete member for deletion.
			return true, nil
		}
		if err := f.encoder.WriteToken(name); err != nil {
			return false, fmt.Errorf("write member name: %w", err)
		}

		// Read and write each unrelated value before the decoder advances
		// and invalidates the raw JSON returned by ReadValue.
		value, err := f.decoder.ReadValue()
		if err != nil {
			return false, fmt.Errorf("read member %q: %w", nameString, err)
		}
		if err := f.encoder.WriteValue(value); err != nil {
			return false, fmt.Errorf("write member %q: %w", nameString, err)
		}
	}
	return false, nil
}

// finish closes a frame after its selected member has been resolved.
// The decoder must be positioned after the member's original value;
// the encoder must already contain the chosen replacement or omission.
func (f *objectRewriteFrame) finish() (jsontext.Value, error) {
	for f.decoder.PeekKind() != '}' {
		name, err := f.decoder.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("read member name: %w", err)
		}
		if err := f.encoder.WriteToken(name); err != nil {
			return nil, fmt.Errorf("write member name: %w", err)
		}
		nameString := name.String()

		value, err := f.decoder.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("read member %q: %w", nameString, err)
		}
		if err := f.encoder.WriteValue(value); err != nil {
			return nil, fmt.Errorf("write member %q: %w", nameString, err)
		}
	}

	end, err := f.decoder.ReadToken()
	if err != nil {
		return nil, fmt.Errorf("read object closing: %w", err)
	}
	if err := f.encoder.WriteToken(end); err != nil {
		return nil, fmt.Errorf("write object closing: %w", err)
	}
	return bytes.TrimSuffix(f.output.Bytes(), []byte("\n")), nil
}

// Increment returns a program that increments the integer at path.
// An absent value starts at zero.
func Increment(path jsontext.Pointer) Program[int64] {
	return Decode[int64](path).Then(func(value int64) Program[int64] {
		if value == math.MaxInt64 {
			return Fail[int64](errors.New("integer overflow"))
		}
		value++
		return Set(
			path,
			jsontext.Value(strconv.FormatInt(value, 10)),
		).Returning(value)
	})
}

// InsertAutoIncrement returns a program that increments the integer at
// counter and inserts value into object under the resulting decimal value.
// An absent counter starts at zero, and an absent object starts empty.
// The parent objects addressed by both pointers must exist.
func InsertAutoIncrement(
	counter, object jsontext.Pointer,
	value jsontext.Value,
) Program[int64] {
	must.Bef(counter.IsValid(), "invalid JSON pointer %q", counter)
	must.Bef(counter != "", "auto-increment counter must not be the document root")
	must.Bef(object.IsValid(), "invalid JSON pointer %q", object)
	must.Bef(object != "", "auto-increment object must not be the document root")
	must.Bef(value.IsValid(), "invalid JSON mutation value")
	value = value.Clone()

	// Derive the member path inside Then so every replay uses the counter value
	// read from that replay's document.
	return Increment(counter).Then(func(id int64) Program[int64] {
		member := object.AppendToken(strconv.FormatInt(id, 10))
		return Block(
			SetIfAbsent(object, jsontext.Value(`{}`)),
			Insert(member, value),
		).Returning(id)
	})
}

// Fail returns a program that always returns err.
func Fail[T any](err error) Program[T] {
	must.Bef(err != nil, "jsonmut.Fail requires an error")
	return Program[T]{
		apply: func(jsontext.Value) (jsontext.Value, T, error) {
			var zero T
			return nil, zero, err
		},
	}
}

func returnValue[T any](value T) Program[T] {
	return Program[T]{
		apply: func(document jsontext.Value) (jsontext.Value, T, error) {
			return document, value, nil
		},
	}
}

func lookup(
	document jsontext.Value,
	pointer jsontext.Pointer,
) (jsontext.Value, bool, error) {
	next, stop := iter.Pull(pointer.Tokens())
	defer stop()

	member, ok := next()
	if !ok {
		return document, true, nil
	}
	value, ok, err := lookupObjectMember(document, member, next)
	if err != nil {
		return nil, false, fmt.Errorf("follow %q: %w", pointer, err)
	}
	return value, ok, nil
}

func lookupObjectMember(
	document jsontext.Value,
	member string,
	next func() (string, bool),
) (jsontext.Value, bool, error) {
	if document.Kind() != '{' {
		return nil, false, fmt.Errorf(
			"expected JSON object, got %v",
			document.Kind(),
		)
	}

	decoder := jsontext.NewDecoder(bytes.NewReader(document))
	if _, err := decoder.ReadToken(); err != nil {
		return nil, false, err
	}
	// Scan only this object level and avoid decoding unrelated values.
	for decoder.PeekKind() != '}' {
		name, err := decoder.ReadToken()
		if err != nil {
			return nil, false, err
		}
		if name.String() != member {
			if err := decoder.SkipValue(); err != nil {
				return nil, false, err
			}
			continue
		}

		value, err := decoder.ReadValue()
		if err != nil {
			return nil, false, err
		}
		childMember, ok := next()
		if !ok {
			return value, true, nil
		}
		// Lookup does not rebuild ancestors,
		// so it can recurse into the selected value and return directly.
		return lookupObjectMember(value, childMember, next)
	}
	return nil, false, nil
}

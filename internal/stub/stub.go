// Package stub provides helper functions to replace global variables
// for testing, and restore them afterwards.
package stub

import (
	"reflect"

	"go.abhg.dev/gs/internal/must"
)

// Value replaces the value of a pointer with a new value,
// and returns a function that restores the original value.
//
// Idiomatic usage will look like this:
//
//	func TestSomething(t *testing.T) {
//		defer stub.Value(&globalVar, newValue)()
//
//		// ...
//	}
func Value[T any](ptr *T, value T) (restore func()) {
	original := *ptr
	*ptr = value
	return func() {
		*ptr = original
	}
}

// Func replaces the value of a function pointer
// with a function that returns the provided values.
// It returns a function that restores the original function.
// If the function has multiple return values, pass them all.
//
// Idiomatic usage will look like this:
//
//	func TestSomething(t *testing.T) {
//		defer stub.StubFunc(&globalFunc, 42)()
//
//		globalFunc() // returns 42
//		// ...
//	}
func Func(fnptr any, rets ...any) (restore func()) {
	fnptrv := reflect.ValueOf(fnptr)
	must.BeEqualf(reflect.Ptr, fnptrv.Kind(), "want pointer, got %v (%T)", fnptrv.Kind(), fnptr)
	fnv := fnptrv.Elem()
	must.BeEqualf(reflect.Func, fnv.Kind(), "want pointer to function, got %v (%T)", fnv.Kind(), fnv)

	fnt := fnv.Type()
	must.BeEqualf(len(rets), fnt.NumOut(), "want %d return values, got %d", fnt.NumOut(), len(rets))

	vals := make([]reflect.Value, fnt.NumOut())
	for i, ret := range rets {
		v := reflect.ValueOf(ret)
		if !v.IsValid() {
			// nil is not a valid value for reflect.ValueOf,
			// but may be passed as a placeholder for zero values.
			v = reflect.New(fnt.Out(i)).Elem()
		}

		must.Bef(v.Type().AssignableTo(fnt.Out(i)), "return %d is not assignable to %v", i, fnt.Out(i))
		vals[i] = v
	}

	original := fnv.Interface()
	stub := reflect.MakeFunc(fnv.Type(), func([]reflect.Value) []reflect.Value {
		return vals
	}).Interface()

	fnv.Set(reflect.ValueOf(stub))
	return func() {
		fnv.Set(reflect.ValueOf(original))
	}
}

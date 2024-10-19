package ui

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"
)

// Deferred is a field that is not constructed
// until initialization time.
//
// This is useful for fields that depend on other fields.
type Deferred struct {
	f  Field
	fn func() Field
}

var _ Field = (*Deferred)(nil)

// Defer defers field construction using the given function.
// If the function returns nil, the field will be ignored.
func Defer(fn func() Field) *Deferred {
	return &Deferred{fn: fn}
}

// Init initializes the field.
func (d *Deferred) Init() tea.Cmd {
	d.f = d.fn()
	if d.f == nil {
		return SkipField
	}

	return d.f.Init()
}

// Title returns the title of the deferred field.
func (d *Deferred) Title() string {
	if d.f == nil {
		return ""
	}
	return d.f.Title()
}

// UnmarshalValue unmarshals the value of the deferred field.
func (d *Deferred) UnmarshalValue(unmarshal func(any) error) error {
	if d.f == nil {
		return errors.New("value provided for uninitialized deferred field")
	}
	return d.f.UnmarshalValue(unmarshal)
}

// Description returns the description of the deferred field.
func (d *Deferred) Description() string {
	if d.f == nil {
		return ""
	}
	return d.f.Description()
}

// Err returns an error if the field is in an error state.
func (d *Deferred) Err() error {
	if d.f == nil {
		return nil
	}
	return d.f.Err()
}

// Render renders the deferred field.
func (d *Deferred) Render(w Writer) {
	if d.f == nil {
		return
	}
	d.f.Render(w)
}

// Update receives a new event.
func (d *Deferred) Update(msg tea.Msg) tea.Cmd {
	if d.f == nil {
		return nil
	}
	return d.f.Update(msg)
}

package uitest

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

func TestRobotView_simple(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "input")
	outputFile := filepath.Join(dir, "output")

	require.NoError(t, os.WriteFile(inputFile, []byte(text.Dedent(`
		"foo"
		===
		"bar"
	`)), 0o644))

	view, err := NewRobotView(inputFile, &RobotViewOptions{
		OutputFile: outputFile,
	})
	require.NoError(t, err)

	field1 := &fakeField{View: "Who are you?"}
	field2 := &fakeField{View: "Where are you going?"}

	_, err = io.WriteString(view, "Title: ")
	require.NoError(t, err)
	require.NoError(t, view.Prompt(field1, field2))
	assert.Equal(t, "foo", field1.GotValue)
	assert.Equal(t, "bar", field2.GotValue)

	require.NoError(t, view.Close())

	got, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	assert.Equal(t, text.Dedent(`
		===
		> Title: Who are you?
		"foo"
		===
		> Where are you going?
		"bar"

	`), string(got))
}

func TestRobotView_persistsState(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "input")
	outputFile := filepath.Join(dir, "output")

	require.NoError(t, os.WriteFile(inputFile, []byte(text.Dedent(`
		"foo"
		===
		"bar"
	`)), 0o644))

	field1 := &fakeField{View: "Who are you?"}
	field2 := &fakeField{View: "Where are you going?"}

	{
		view1, err := NewRobotView(inputFile, &RobotViewOptions{
			OutputFile: outputFile,
		})
		require.NoError(t, err)

		_, err = io.WriteString(view1, "INFO: something happened\n")
		require.NoError(t, err)

		require.NoError(t, view1.Prompt(field1))
		require.NoError(t, view1.Close())
	}

	{
		view2, err := NewRobotView(inputFile, &RobotViewOptions{
			OutputFile: outputFile,
		})
		require.NoError(t, err)

		require.NoError(t, view2.Prompt(field2))

		_, err = io.WriteString(view2, "INFO: something else\n")
		require.NoError(t, err)

		require.NoError(t, view2.Close())
	}

	assert.Equal(t, "foo", field1.GotValue)
	assert.Equal(t, "bar", field2.GotValue)

	got, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	assert.Equal(t, text.Dedent(`
		===
		> INFO: something happened
		> Who are you?
		"foo"
		===
		> Where are you going?
		"bar"
		===
		> INFO: something else

	`), string(got))
}

type fakeField struct {
	GotValue any
	View     string
}

var _ ui.Field = (*fakeField)(nil)

func (f *fakeField) Init() tea.Cmd { return nil }
func (f *fakeField) Err() error    { return nil }

func (f *fakeField) Description() string { return "" }
func (f *fakeField) Title() string       { return "" }

func (f *fakeField) Update(msg tea.Msg) tea.Cmd { return nil }

func (f *fakeField) Render(w ui.Writer) {
	_, _ = w.WriteString(f.View)
}

func (f *fakeField) UnmarshalValue(unmarshal func(any) error) error {
	return unmarshal(&f.GotValue)
}

func TestRobotFile(t *testing.T) {
	tests := []struct {
		name string
		give string
		want robotFixtureFile
	}{
		{name: "Empty"},
		{
			name: "Single",
			give: text.Dedent(`
				> foo
				"bar"
			`),
			want: robotFixtureFile{
				{
					Comment: "foo",
					Value:   `"bar"` + "\n",
				},
			},
		},
		{
			name: "SingleCommentOnlySection",
			give: text.Dedent(`
				> foo
			`),
			want: robotFixtureFile{
				{
					Comment: "foo",
				},
			},
		},
		{
			name: "Multiple",
			give: text.Dedent(`
				> foo
				> another
				"bar"
				===
				> baz
				"qux"
			`),
			want: robotFixtureFile{
				{
					Comment: "foo\nanother",
					Value:   `"bar"` + "\n",
				},
				{
					Comment: "baz",
					Value:   `"qux"` + "\n",
				},
			},
		},
		{
			name: "MultipleWithCommentOnly",
			give: text.Dedent(`
				> foo
				"bar"
				===
				> baz
				===
				"qux"
			`),
			want: robotFixtureFile{
				{
					Comment: "foo",
					Value:   `"bar"` + "\n",
				},
				{
					Comment: "baz",
				},
				{
					Value: `"qux"` + "\n",
				},
			},
		},
		{
			name: "EmptyLineInComment",
			give: text.Dedent(`
				> foo
				>
				> bar
				"baz"
			`),
			want: robotFixtureFile{
				{
					Comment: "foo\n\nbar",
					Value:   `"baz"` + "\n",
				},
			},
		},
		{
			name: "NoComment",
			give: text.Dedent(`
				"foo"
			`),
			want: robotFixtureFile{
				{
					Value: `"foo"` + "\n",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got robotFixtureFile
			require.NoError(t, got.Read(strings.NewReader(tt.give)))
			require.Equal(t, tt.want, got)

			t.Run("RoundTrip", func(t *testing.T) {
				var b strings.Builder
				require.NoError(t, got.Write(&b))

				var got2 robotFixtureFile
				require.NoError(t, got2.Read(strings.NewReader(b.String())))

				assert.Equal(t, got, got2,
					"round trip failed:\ngave: %q\ngot: %q", tt.give, b.String())
			})
		})
	}
}

func FuzzRobotFile_Read(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte(text.Dedent(`
		> foo
		"bar"
	`)))
	f.Add([]byte(text.Dedent(`
		> foo
		"bar"
		===
		> baz
		"qux"
	`)))

	f.Fuzz(func(t *testing.T, data []byte) {
		var sf robotFixtureFile
		_ = sf.Read(bytes.NewReader(data))
		// Just make sure it doesn't panic or infinite loop.
	})
}

func FuzzRobotFile_roundTrip(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte(text.Dedent(`
		> foo
		"bar"
	`)))
	f.Add([]byte(text.Dedent(`
		> foo
		"bar"
		===
		> baz
		"qux"
	`)))

	f.Fuzz(func(t *testing.T, data []byte) {
		var give robotFixtureFile
		if err := give.Read(bytes.NewReader(data)); err != nil {
			t.Skip("invalid input")
		}

		for i, f := range give {
			var body any
			if err := json.Unmarshal([]byte(f.Value), &body); err != nil {
				t.Skip("invalid input")
			}

			bs, err := json.Marshal(body)
			require.NoError(t, err)

			f.Value = string(bs) + "\n"
			give[i] = f
		}

		var b bytes.Buffer
		require.NoError(t, give.Write(&b))

		var got robotFixtureFile
		require.NoError(t, got.Read(&b))

		assert.Equal(t, give, got)
	})
}

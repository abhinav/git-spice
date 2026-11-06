package submit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageFile_Decode(t *testing.T) {
	var cli struct {
		Files []MessageFile `name:"message-file" sep:"none"`
	}
	parser, err := kong.New(&cli)
	require.NoError(t, err)
	_, err = parser.Parse([]string{
		"--message-file", "feature=one:missing,file:one.md",
		"--message-file", "feature-two:missing two.md",
	})
	require.NoError(t, err)
	assert.Equal(t, []MessageFile{
		{Branch: "feature=one", Path: "missing,file:one.md"},
		{Branch: "feature-two", Path: "missing two.md"},
	}, cli.Files)
}

func TestMessageFile_Decode_invalid(t *testing.T) {
	for _, spec := range []string{"missing-separator", ":file.md", "branch:"} {
		t.Run(spec, func(t *testing.T) {
			var cli struct {
				File MessageFile `name:"message-file"`
			}
			parser, err := kong.New(&cli)
			require.NoError(t, err)
			_, err = parser.Parse([]string{"--message-file", spec})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "BRANCH:PATH")
		})
	}
}

func TestMessageFile_read(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		title    string
		body     string
	}{
		{name: "TitleOnly", contents: "Only a title\n", title: "Only a title"},
		{name: "MultilineBody", contents: "A title\n\nFirst paragraph.\n\n    Indented text.\n", title: "A title", body: "First paragraph.\n\n    Indented text."},
		{name: "IndentedBody", contents: "A title\n\n    Code.\n", title: "A title", body: "    Code."},
		{name: "WindowsNewlines", contents: "A title\r\n\r\nA body.\r\n", title: "A title", body: "A body."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "message.md")
			require.NoError(t, os.WriteFile(path, []byte(tt.contents), 0o600))
			message, err := (MessageFile{Branch: "feature", Path: path}).read()
			require.NoError(t, err)
			assert.Equal(t, tt.title, message.Title)
			assert.Equal(t, tt.body, message.Body)
		})
	}
}

func TestMessageFile_read_invalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message.md")
	file := MessageFile{Branch: "feature", Path: path}
	_, err := file.read()
	require.Error(t, err)
	assert.Contains(t, err.Error(), path)
	for _, contents := range []string{"", " \nA body without a title."} {
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
		_, err = file.read()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "title")
	}
}

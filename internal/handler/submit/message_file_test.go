package submit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageFiles_Decode(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "message,file:one.md")
	second := filepath.Join(dir, "message two.md")
	require.NoError(t, os.WriteFile(first, nil, 0o600))
	require.NoError(t, os.WriteFile(second, nil, 0o600))
	var opts BatchOptions
	parser, err := kong.New(&opts)
	require.NoError(t, err)
	_, err = parser.Parse([]string{
		"--message-file", "feature=one:" + first,
		"--message-file", "feature-two:" + second,
	})
	require.NoError(t, err)
	assert.Equal(t, MessageFiles{
		"feature=one": MessageFile(first),
		"feature-two": MessageFile(second),
	}, opts.MessageFiles)
}

func TestMessageFiles_Decode_duplicateBranch(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.md")
	second := filepath.Join(dir, "second.md")
	require.NoError(t, os.WriteFile(first, nil, 0o600))
	require.NoError(t, os.WriteFile(second, nil, 0o600))
	var opts BatchOptions
	parser, err := kong.New(&opts)
	require.NoError(t, err)
	_, err = parser.Parse([]string{
		"--message-file", "feature:" + first,
		"--message-file", "feature:" + second,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `message file already supplied for branch "feature"`)
}

func TestMessageFiles_Decode_invalid(t *testing.T) {
	tests := []struct {
		spec string
		want string
	}{
		{spec: "missing-separator", want: `must be in BRANCH:PATH form, got: "missing-separator"`},
		{spec: ":file.md", want: "branch name must be supplied"},
		{spec: "branch:", want: "path must be supplied"},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			var opts BatchOptions
			parser, err := kong.New(&opts)
			require.NoError(t, err)
			_, err = parser.Parse([]string{"--message-file", tt.spec})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestMessageFile_Decode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message.md")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	var cli struct {
		File MessageFile `name:"message-file"`
	}
	parser, err := kong.New(&cli)
	require.NoError(t, err)
	_, err = parser.Parse([]string{"--message-file", path})
	require.NoError(t, err)
	assert.Equal(t, path, string(cli.File))
}

func TestMessageFile_Decode_invalid(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "Empty", want: "path must be supplied"},
		{name: "Missing", path: filepath.Join(dir, "missing"), want: "missing"},
		{name: "Directory", path: dir, want: "must be a regular file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cli struct {
				File MessageFile `name:"message-file"`
			}
			parser, err := kong.New(&cli)
			require.NoError(t, err)
			_, err = parser.Parse([]string{"--message-file", tt.path})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
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
			title, body, err := MessageFile(path).read()
			require.NoError(t, err)
			assert.Equal(t, tt.title, title)
			assert.Equal(t, tt.body, body)
		})
	}
}

func TestMessageFile_read_invalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message.md")
	file := MessageFile(path)
	_, _, err := file.read()
	require.Error(t, err)
	assert.Contains(t, err.Error(), path)
	for _, contents := range []string{"", " \nA body without a title."} {
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
		_, _, err = file.read()
		require.Error(t, err)
		assert.EqualError(t, err, "must start with a non-empty title")
	}
}

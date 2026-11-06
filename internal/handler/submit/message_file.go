package submit

import (
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/kong"
)

// MessageFile associates a branch with a file containing its new CR's message.
// Decoding records the path; the file is read only when creating the CR.
type MessageFile struct {
	Branch string
	Path   string
}

// Decode parses BRANCH:PATH without opening the file.
func (f *MessageFile) Decode(ctx *kong.DecodeContext) error {
	var spec string
	if err := ctx.Scan.PopValueInto("message-file", &spec); err != nil {
		return err
	}
	branch, path, ok := strings.Cut(spec, ":")
	if !ok || branch == "" || path == "" {
		return fmt.Errorf("expected BRANCH:PATH with a non-empty branch and path, got %q", spec)
	}
	f.Branch = branch
	f.Path = path
	return nil
}

func (f MessageFile) read() (changeMessage, error) {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return changeMessage{}, fmt.Errorf("read message file %q: %w", f.Path, err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	title, body, _ := strings.Cut(text, "\n")
	title = strings.TrimSpace(title)
	if title == "" {
		return changeMessage{}, fmt.Errorf("message file %q must start with a non-empty title", f.Path)
	}
	return changeMessage{
		Title: title,
		Body:  strings.Trim(body, "\n"),
	}, nil
}

type changeMessage struct {
	Title string
	Body  string
}

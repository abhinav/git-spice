package submit

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/kong"
)

// MessageFiles maps branch names to files containing their new CR messages.
type MessageFiles map[string]*MessageFile // branch -> MessageFile

// Decode parses a BRANCH:PATH pair and adds it to the map.
func (files *MessageFiles) Decode(ctx *kong.DecodeContext) error {
	var spec string
	if err := ctx.Scan.PopValueInto("message-file", &spec); err != nil {
		return err
	}
	// Kong's map decoder fixes the key/value separator at '='.
	// Split here to support ':' while allowing '=' in branch names.
	branch, path, ok := strings.Cut(spec, ":")
	if !ok {
		return fmt.Errorf("must be in BRANCH:PATH form, got: %q", spec)
	}
	if branch == "" {
		return errors.New("branch name must be supplied")
	}
	file := new(MessageFile)
	if err := file.Decode(ctx.WithScanner(kong.ScanAsType(kong.FlagValueToken, path))); err != nil {
		return err
	}
	if *files == nil {
		*files = make(MessageFiles)
	}
	(*files)[branch] = file
	return nil
}

// MessageFile identifies a regular file containing a new CR's title and body.
// Decoding validates the path; contents are read only when creating the CR.
type MessageFile struct {
	Path string
}

// Decode validates a message file's path without reading its contents.
func (f *MessageFile) Decode(ctx *kong.DecodeContext) error {
	var path string
	if err := ctx.Scan.PopValueInto("message-file", &path); err != nil {
		return err
	}
	if path == "" {
		return errors.New("path must be supplied")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat message file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("message file %q must be a regular file", path)
	}
	f.Path = path
	return nil
}

func (f *MessageFile) read() (title, body string, _ error) {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return "", "", fmt.Errorf("read message file %q: %w", f.Path, err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	title, body, _ = strings.Cut(text, "\n")
	title = strings.TrimSpace(title)
	if title == "" {
		return "", "", fmt.Errorf("message file %q must start with a non-empty title", f.Path)
	}
	return title, strings.Trim(body, "\n"), nil
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"go.abhg.dev/gs/internal/handler/submit"
)

// parseFillFrom reads per-branch CR metadata from a file or stdin.
// If path is "-", it reads from stdin.
// The expected format is a JSON object mapping branch names
// to objects with "title" and "body" fields.
func parseFillFrom(path string) (map[string]submit.BranchMeta, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var meta map[string]submit.BranchMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return meta, nil
}

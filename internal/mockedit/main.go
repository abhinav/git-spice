// Package mockedit provides a mock implementation of an editor.
// It's a a simple process controlled with environment variables:
//
//   - MOCKEDIT_GIVE:
//     Specifies the path to a file that contains the contents
//     to write for an edit operation.
//     This is required.
//   - MOCKEDIT_RECORD:
//     Specifies the path to a file where contents of an edited file
//     should be written.
//     This is optional.
//
// The process expects the path to a file to edit as its only argument.
package mockedit

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/osutil"
)

// Main runs the mock editor and exits the process.
// Usage:
//
//	mockedit <file>
//
// mockedit writes the contents of MOCKEDIT_GIVE into the given file.
// If MOCKEDIT_GIVE is not set, the file is returned unchanged.
// If MOCKEDIT_RECORD is set, it will also copy the contents of <file> into it.
//
// If both MOCKEDIT_GIVE and MOCKEDIT_RECORD are unset, mockedit will fail.
func Main() {
	log.SetFlags(0)
	flag.Parse()

	if flag.NArg() != 1 {
		log.Fatal("usage: mockedit file")
	}

	input := flag.Arg(0)

	data, err := os.ReadFile(input)
	if err != nil {
		log.Fatalf("read %s: %s", input, err)
	}

	give := os.Getenv("MOCKEDIT_GIVE")
	record := os.Getenv("MOCKEDIT_RECORD")
	if give == "" && record == "" {
		log.Fatalf("unexpected edit, got:\n%s", string(data))
	}

	if record != "" {
		if err := os.WriteFile(record, data, 0o644); err != nil {
			log.Fatalf("write %s: %s", record, err)
		}
	}

	if give != "" {
		bs, err := os.ReadFile(give)
		if err != nil {
			log.Fatalf("read %s: %s", give, err)
		}

		if err := os.WriteFile(input, bs, 0o644); err != nil {
			log.Fatalf("write %s: %s", input, err)
		}
	}
}

// Handle controls the behavior of the mock editor.
type Handle struct {
	t testing.TB

	dir    string // temporary working directory
	record string // file to record the input
}

// Expect tells mockedit to expect a new edit operation.
//
// By default, following an Expect call,
// mockedit will write back the file unchanged.
//
// Use Give to specify the contents to write back.
func Expect(t testing.TB) *Handle {
	dir := t.TempDir()
	record, err := osutil.TempFilePath(dir, "mockedit-record")
	require.NoError(t, err)

	t.Setenv("EDITOR", "mockedit")
	t.Setenv("MOCKEDIT_RECORD", record)

	return &Handle{
		t:      t,
		dir:    dir,
		record: record,
	}
}

// ExpectNone is a convenience method to expect no edits
// for the remainder of the test, or until the next Expect call.
func ExpectNone(t testing.TB) {
	t.Setenv("EDITOR", "mockedit")
	t.Setenv("MOCKEDIT_RECORD", "")
	t.Setenv("MOCKEDIT_GIVE", "")
}

// Give tells mockedit to write the given contents back
// for the next edit operation.
func (e *Handle) Give(contents string) *Handle {
	giveFile := filepath.Join(e.dir, "mockedit-give")
	require.NoError(e.t, os.WriteFile(giveFile, []byte(contents), 0o644))
	e.t.Setenv("MOCKEDIT_GIVE", giveFile)
	return e
}

// GiveLines is a convenience method to give multiple lines of contents.
func (e *Handle) GiveLines(lines ...string) *Handle {
	var s strings.Builder
	for _, line := range lines {
		fmt.Fprintln(&s, line)
	}
	return e.Give(s.String())
}

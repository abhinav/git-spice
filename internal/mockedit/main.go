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
	"log"
	"os"
)

// Run runs the mock editor and exits the process.
func Run() {
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

	if record := os.Getenv("MOCKEDIT_RECORD"); record != "" {
		if err := os.WriteFile(record, data, 0o644); err != nil {
			log.Fatalf("write %s: %s", record, err)
		}
	}

	give := os.Getenv("MOCKEDIT_GIVE")
	if give == "" {
		log.Fatalf("unexpected edit, got:\n%s", string(data))
	}

	bs, err := os.ReadFile(give)
	if err != nil {
		log.Fatalf("read %s: %s", give, err)
	}

	if err := os.WriteFile(input, bs, 0o644); err != nil {
		log.Fatalf("write %s: %s", input, err)
	}

	os.Exit(0)
}

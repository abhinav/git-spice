package fixup

import (
	"log"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("FIXUP_EDITOR_HELPER") == "1" {
		fixupEditorHelper()
		return
	}

	os.Exit(m.Run())
}

func fixupEditorHelper() {
	log.SetFlags(0)
	if len(os.Args) != 2 {
		log.Fatal("usage: fixup-editor-helper file")
	}

	content, err := os.ReadFile(os.Getenv("FIXUP_EDITOR_GIVE"))
	if err != nil {
		log.Fatalf("read content: %v", err)
	}
	if err := os.WriteFile(os.Args[1], content, 0o644); err != nil {
		log.Fatalf("write target file: %v", err)
	}
}

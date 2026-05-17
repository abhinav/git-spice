package gitedit

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
	"go.abhg.dev/gs/internal/mockedit"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		// mockedit replaces its file argument
		// with MOCKEDIT_GIVE and records the original
		// file at MOCKEDIT_RECORD.
		"mockedit": mockedit.Main,

		// hook-helper appends HOOK_APPEND
		// to its file argument and exits with HOOK_EXIT_CODE.
		"hook-helper": hookHelperMain,

		// slowedit waits on sentinel files
		// before copying SLOWEDIT_GIVE to its file argument.
		"slowedit": sloweditMain,

		// editor-env-helper verifies inherited and added
		// environment variables before copying EDITOR_ENV_GIVE.
		"editor-env-helper": editorEnvHelperMain,

		// editor-copy copies EDITOR_COPY_GIVE
		// to its file argument.
		"editor-copy": editorCopyMain,

		// editor-fail exits with a non-zero status.
		"editor-fail": editorFailMain,

		// editor-verify checks that its file argument
		// contains EDITOR_VERIFY_WANT before copying EDITOR_COPY_GIVE.
		"editor-verify": editorVerifyMain,
	})
}

// hookHelperMain is a controllable hook helper program
// for use in integration tests.
// Its behavior is driven by environment variables:
//
//   - HOOK_EXIT_CODE: exit with this code (default 0).
//   - HOOK_APPEND: append this string to the file
//     given as the first argument ($1).
func hookHelperMain() {
	log.SetFlags(0)
	flag.Parse()

	if exitCode := os.Getenv("HOOK_EXIT_CODE"); exitCode != "" {
		code := 0
		if _, err := fmt.Sscanf(exitCode, "%d", &code); err != nil {
			log.Fatalf("parse HOOK_EXIT_CODE: %v", err)
		}
		os.Exit(code)
	}

	if appendStr := os.Getenv("HOOK_APPEND"); appendStr != "" {
		if flag.NArg() < 1 {
			log.Fatal("hook-helper: no file argument")
		}
		filePath := flag.Arg(0)
		f, err := os.OpenFile(
			filePath,
			os.O_APPEND|os.O_WRONLY,
			0o644,
		)
		if err != nil {
			log.Fatalf("open %s: %v", filePath, err)
		}
		defer func() { _ = f.Close() }()

		_, _ = fmt.Fprintln(f, appendStr)
	}
}

// sloweditMain is a fake editor that coordinates
// with a parent test process via sentinel files.
// It is used to test signal handling during editor execution.
//
// Environment variables:
//
//   - SLOWEDIT_READY: path to create when editor is ready
//   - SLOWEDIT_CONTINUE: path to poll for before completing
//   - SLOWEDIT_GIVE: path to file whose contents
//     will be written to the target file
func sloweditMain() {
	log.SetFlags(0)
	flag.Parse()

	if flag.NArg() != 1 {
		log.Fatal("usage: slowedit file")
	}

	ready := os.Getenv("SLOWEDIT_READY")
	cont := os.Getenv("SLOWEDIT_CONTINUE")
	give := os.Getenv("SLOWEDIT_GIVE")
	if ready == "" || cont == "" || give == "" {
		log.Fatal("SLOWEDIT_READY, SLOWEDIT_CONTINUE, " +
			"and SLOWEDIT_GIVE must be set")
	}

	// Signal readiness by creating the sentinel file.
	if err := os.WriteFile(ready, nil, 0o644); err != nil {
		log.Fatalf("write ready sentinel: %v", err)
	}

	// Poll for continue sentinel.
	for {
		if _, err := os.Stat(cont); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Write the given content to the target file.
	content, err := os.ReadFile(give)
	if err != nil {
		log.Fatalf("read give file: %v", err)
	}
	if err := os.WriteFile(flag.Arg(0), content, 0o644); err != nil {
		log.Fatalf("write target file: %v", err)
	}
}

func editorEnvHelperMain() {
	log.SetFlags(0)
	flag.Parse()

	if flag.NArg() != 1 {
		log.Fatal("usage: editor-env-helper file")
	}

	for _, env := range []struct {
		name string
		want string
	}{
		{
			name: os.Getenv("EDITOR_ENV_INHERITED_NAME"),
			want: os.Getenv("EDITOR_ENV_INHERITED_WANT"),
		},
		{
			name: os.Getenv("EDITOR_ENV_ADDED_NAME"),
			want: os.Getenv("EDITOR_ENV_ADDED_WANT"),
		},
	} {
		if got := os.Getenv(env.name); got != env.want {
			log.Fatalf("%s = %q, want %q", env.name, got, env.want)
		}
	}

	content, err := os.ReadFile(os.Getenv("EDITOR_ENV_GIVE"))
	if err != nil {
		log.Fatalf("read content: %v", err)
	}
	if err := os.WriteFile(flag.Arg(0), content, 0o644); err != nil {
		log.Fatalf("write target file: %v", err)
	}
}

func editorCopyMain() {
	log.SetFlags(0)
	flag.Parse()

	if flag.NArg() != 1 {
		log.Fatal("usage: editor-copy file")
	}

	content, err := os.ReadFile(os.Getenv("EDITOR_COPY_GIVE"))
	if err != nil {
		log.Fatalf("read content: %v", err)
	}
	if err := os.WriteFile(flag.Arg(0), content, 0o644); err != nil {
		log.Fatalf("write target file: %v", err)
	}
}

func editorFailMain() {
	os.Exit(1)
}

func editorVerifyMain() {
	log.SetFlags(0)
	flag.Parse()

	if flag.NArg() != 1 {
		log.Fatal("usage: editor-verify file")
	}

	content, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatalf("read target file: %v", err)
	}
	if want := os.Getenv("EDITOR_VERIFY_WANT"); !strings.Contains(string(content), want) {
		log.Fatalf("target file does not contain %q", want)
	}

	content, err = os.ReadFile(os.Getenv("EDITOR_COPY_GIVE"))
	if err != nil {
		log.Fatalf("read content: %v", err)
	}
	if err := os.WriteFile(flag.Arg(0), content, 0o644); err != nil {
		log.Fatalf("write target file: %v", err)
	}
}

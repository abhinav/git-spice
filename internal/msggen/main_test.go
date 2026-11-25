package msggen

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("MSGGEN_TEST_HELPER"); mode != "" {
		if mode == "exit" {
			os.Exit(7)
		}
		if err := runMessageTestHelper(mode, os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	m.Run()
}

func runMessageTestHelper(mode string, args []string) error {
	if mode == "edit" {
		if len(args) != 2 || args[1] != "commit-create" {
			return fmt.Errorf("unexpected edit arguments: %q", args)
		}
		if os.Getenv("GIT_EDITOR") != "original-editor" {
			return fmt.Errorf("editor environment was not restored: %q", os.Getenv("GIT_EDITOR"))
		}
		if _, ok := os.LookupEnv(_fillCommandEnv); ok {
			return errors.New("internal fill command leaked to the editor")
		}
		fmt.Fprintln(os.Stderr, "Question?")
		answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "Progress")
		return os.WriteFile(args[0], []byte(answer), 0o600)
	}
	if len(args) != 4 {
		return fmt.Errorf("unexpected range arguments: %q", args)
	}
	if mode == "unchanged" {
		return nil
	}
	draft, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	if len(draft) != 0 {
		return fmt.Errorf("range draft was not empty: %q", draft)
	}
	switch mode {
	case "range":
		if !slices.Equal(args[1:], []string{"branch-squash", "base", "head"}) {
			return fmt.Errorf("unexpected range: %q", args[1:])
		}
		return os.WriteFile(args[0], []byte("Subject\n\nBody\n"), 0o600)
	case "stdout-only":
		fmt.Fprintln(os.Stdout, "only stdout")
		return nil
	case "blank-subject":
		return os.WriteFile(args[0], []byte("  \nBody"), 0o600)
	default:
		return fmt.Errorf("unknown message helper mode %q", mode)
	}
}

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
)

// runMockMessage is a message command controlled by test-script fixtures.
// MOCK_MESSAGE_RESPONSE names a template with Operation and Subject fields.
// MOCK_MESSAGE_RECORD optionally names a directory for the original draft,
// invocation arguments, and Git state observed by the command.
// MOCK_MESSAGE_EXIT requests a nonzero exit without editing the message.
func runMockMessage() int {
	if value := os.Getenv("MOCK_MESSAGE_EXIT"); value != "" {
		code, err := strconv.Atoi(value)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return code
	}
	if err := mockMessage(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func mockMessage(args []string) error {
	if len(args) != 2 && len(args) != 4 {
		return fmt.Errorf("expected a message file, operation, and optional range; got %q", args)
	}
	ctx := context.Background()
	wt, err := git.OpenWorktree(ctx, ".", git.OpenOptions{Log: silog.Nop()})
	if err != nil {
		return err
	}
	head, err := wt.Head(ctx)
	if err != nil {
		return err
	}
	subjectRef := head.String()
	if len(args) == 4 {
		subjectRef = args[3]
	}
	commit, err := wt.Repository().ReadCommit(ctx, subjectRef)
	if err != nil {
		return err
	}

	if dir := os.Getenv("MOCK_MESSAGE_RECORD"); dir != "" {
		draft, err := os.ReadFile(args[0])
		if err != nil {
			return err
		}
		staged, err := wt.DiffIndex(ctx, "HEAD")
		if err != nil {
			return err
		}
		var stagedPaths, unstagedPaths strings.Builder
		for _, file := range staged {
			fmt.Fprintln(&stagedPaths, file.Path)
		}
		for file, err := range wt.DiffWork(ctx) {
			if err != nil {
				return err
			}
			fmt.Fprintln(&unstagedPaths, file.Path)
		}
		selectedRange := ""
		if len(args) == 4 {
			selectedRange = strings.Join(args[2:], "\n") + "\n"
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		for name, data := range map[string]string{
			"operation": args[1] + "\n",
			"draft":     string(draft),
			"head":      head.String() + "\n",
			"subject":   commit.Subject + "\n",
			"staged":    stagedPaths.String(),
			"unstaged":  unstagedPaths.String(),
			"range":     selectedRange,
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
				return err
			}
		}
	}
	response, err := os.ReadFile(os.Getenv("MOCK_MESSAGE_RESPONSE"))
	if err != nil {
		return err
	}
	tmpl, err := template.New("message").Parse(string(response))
	if err != nil {
		return err
	}
	var message bytes.Buffer
	if err := tmpl.Execute(&message, struct{ Operation, Subject string }{
		Operation: args[1],
		Subject:   commit.Subject,
	}); err != nil {
		return err
	}
	if err := os.WriteFile(args[0], message.Bytes(), 0o600); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "Generator progress")
	fmt.Fprintln(os.Stderr, "Generator diagnostic")
	return nil
}

// mockMessageHook installs or runs a post-commit hook to check the installed index.
func mockMessageHook(args []string) error {
	if len(args) == 1 && args[0] == "install" {
		// Git runs this forwarding script on Unix and Windows; the checks stay in Go.
		return os.WriteFile(filepath.Join(".git", "hooks", "post-commit"), []byte("#!/bin/sh\nmock-message-hook\n"), 0o700)
	}
	if len(args) != 0 {
		return fmt.Errorf("unexpected hook arguments: %q", args)
	}
	ctx := context.Background()
	wt, err := git.OpenWorktree(ctx, ".", git.OpenOptions{Log: silog.Nop()})
	if err != nil {
		return err
	}
	active, err := wt.IndexFile(ctx)
	if err != nil {
		return err
	}
	if err := os.Unsetenv("GIT_INDEX_FILE"); err != nil {
		return err
	}
	installed, err := wt.IndexFile(ctx)
	if err != nil {
		return err
	}
	if active != installed {
		return fmt.Errorf("post-commit index %q differs from installed index %q", active, installed)
	}
	staged, err := wt.DiffIndex(ctx, "HEAD")
	if err != nil {
		return err
	}
	if len(staged) != 0 {
		return fmt.Errorf("post-commit saw staged changes: %v", staged)
	}
	fmt.Fprintln(os.Stderr, "post-commit saw the installed index")
	return nil
}

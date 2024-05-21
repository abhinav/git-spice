// Package gittest provides utilities for testing git repositories.
package gittest

import (
	"net/mail"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

// CmdGit runs a git command in the repository.
//
//	[!] git [args ...]
func CmdGit(ts *testscript.TestScript, neg bool, args []string) {
	err := ts.Exec("git", args...)
	if neg {
		if err == nil {
			ts.Fatalf("unexpected success, expected failure")
		}
	} else {
		ts.Check(err)
	}
}

// CmdAs sets the author and committer of the commits that follow.
//
//	as 'User Name <user@example.com>'
func CmdAs(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: as 'User Name <user@example.com>'")
	}

	addr, err := mail.ParseAddress(args[0])
	if err != nil {
		ts.Fatalf("invalid email address: %s", err)
	}

	ts.Setenv("GIT_AUTHOR_NAME", addr.Name)
	ts.Setenv("GIT_AUTHOR_EMAIL", addr.Address)
	ts.Setenv("GIT_COMMITTER_NAME", addr.Name)
	ts.Setenv("GIT_COMMITTER_EMAIL", addr.Address)
}

// CmdAt sets the author and commit time of the commits that follow.
//
//	at <YYYY-MM-DDTHH:MM:SS>
func CmdAt(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: at <YYYY-MM-DDTHH:MM:SS>")
	}

	t, err := time.Parse(time.RFC3339, args[0])
	if err != nil {
		ts.Fatalf("invalid time: %s", err)
	}

	gitTime := t.Format(time.RFC3339)
	ts.Setenv("GIT_AUTHOR_DATE", gitTime)
	ts.Setenv("GIT_COMMITTER_DATE", gitTime)
}

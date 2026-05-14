package main

import (
	"context"
	"strconv"

	"go.abhg.dev/gs/internal/git"
)

// msggenEnv builds the base environment variables
// for message generator scripts.
// kind is the message kind (e.g. "commit" or "branch").
// update indicates whether this is an update
// of an existing message.
// extras are appended as additional environment variables.
func msggenEnv(
	kind string, update bool, extras ...string,
) []string {
	env := make([]string, 0, 2+len(extras))
	env = append(env,
		"GS_MESSAGE_KIND="+kind,
		"GS_MESSAGE_UPDATE="+strconv.FormatBool(update),
	)
	return append(env, extras...)
}

// commitEnv builds environment variables
// for commit message scripts.
// It calls msggenEnv and appends GS_BRANCH
// when available.
func commitEnv(
	ctx context.Context,
	wt *git.Worktree,
	update bool,
	extras ...string,
) []string {
	env := msggenEnv("commit", update, extras...)
	if branch, err := wt.CurrentBranch(ctx); err == nil {
		env = append(env, "GS_BRANCH="+branch)
	}
	return env
}

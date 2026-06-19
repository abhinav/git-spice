package main

import (
	"context"
	"strconv"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/scriptrun"
)

// msggenEnv builds the base environment for a message-generation
// script. It layers the message-specific GS_MESSAGE_* variables on
// top of the shared GS_OPERATION / GS_BRANCH / GS_BASE env that every
// gs-driven script receives.
//
// kind names the operation (one of [scriptrun.Operation]). update
// indicates whether this is an update of an existing message (e.g.
// 'gs commit amend --fill', 'gs branch submit --fill' updating an
// existing CR). branch is the active branch when known; base is the
// branch's base when applicable. extras are appended verbatim.
func msggenEnv(
	op scriptrun.Operation,
	update bool,
	branch, base string,
	extras ...string,
) []string {
	env := scriptrun.EnvFor(op, branch, base)
	env = append(env, "GS_MESSAGE_UPDATE="+strconv.FormatBool(update))
	return append(env, extras...)
}

// commitEnv builds the environment for a commit-message script.
// update is true when amending an existing message.
func commitEnv(
	ctx context.Context,
	wt *git.Worktree,
	op scriptrun.Operation,
	update bool,
	extras ...string,
) []string {
	branch := ""
	if b, err := wt.CurrentBranch(ctx); err == nil {
		branch = b
	}
	return msggenEnv(op, update, branch, "", extras...)
}

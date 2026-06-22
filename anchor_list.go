package main

import (
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type anchorListCmd struct{}

func (*anchorListCmd) Help() string {
	return text.Dedent(`
		Lists the anchors registered in the repository.

		For each anchor, shows its branch, the worktree that owns it, and
		whether it is a root anchor (tracking the canonical trunk) or an
		internal anchor (pinned at another local branch). The anchor that
		is the trunk in effect for the current worktree is marked with a
		'*'.
	`)
}

func (*anchorListCmd) Run(
	log *silog.Logger,
	store *state.Store,
	wt *git.Worktree,
) error {
	anchors := store.Anchors()
	if len(anchors) == 0 {
		log.Infof("No anchors in this repository")
		return nil
	}

	// The anchor in effect for the current worktree, if any, is marked.
	currentAnchor := store.TrunkFor(wt.RootDir())

	for _, a := range anchors {
		marker := " "
		if a.Branch == currentAnchor {
			marker = "*"
		}

		kind := "root"
		if a.Base != "" {
			kind = "on " + a.Base
		}

		worktree := a.Worktree
		if worktree == "" {
			worktree = "(unknown)"
		}

		log.Infof("%s %s\t%s\t(%s)", marker, a.Branch, worktree, kind)
	}

	return nil
}

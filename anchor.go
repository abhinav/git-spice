package main

// anchorCmd groups commands that manage worktree anchors: per-worktree
// pointer branches that hold a slice of the stack in place so parallel
// processes (agents, CI, the user) can work in one repository without
// clobbering each other.
type anchorCmd struct {
	Create anchorCreateCmd `cmd:"" aliases:"c" help:"Create a new worktree anchored at a branch"`
	List   anchorListCmd   `cmd:"" aliases:"ls" help:"List anchors and their worktrees"`
	Track  anchorTrackCmd  `cmd:"" aliases:"tr" help:"Track an existing worktree as an anchor"`
	Rm     anchorRmCmd     `cmd:"" aliases:"rm" help:"Remove an anchor worktree and dissolve its anchor"`
}

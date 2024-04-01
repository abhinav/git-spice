package main

type branchCmd struct {
	Track   branchTrackCmd   `cmd:"" aliases:"tr" help:"Begin tracking a branch with gs"`
	Untrack branchUntrackCmd `cmd:"" aliases:"utr" help:"Stop tracking a branch with gs"`

	// Creation and destruction
	Create branchCreateCmd `cmd:"" aliases:"c" help:"Create a new branch"`
	Delete branchDeleteCmd `cmd:"" aliases:"rm" help:"Delete the current branch"`
	Pop    branchPopCmd    `cmd:"" help:"Delete a branch but keep its changes"`
	Fold   branchFoldCmd   `cmd:"" aliases:"f" help:"Fold a branch into its base"`
	Split  branchSplitCmd  `cmd:"" aliases:"sp" help:"Split the current branch into multiple"`

	// Mutation
	Edit    branchEditCmd    `cmd:"" aliases:"e" help:"Edit the commits in the current branch"`
	Rename  branchRenameCmd  `cmd:"" aliases:"mv" help:"Rename the current branch"`
	Restack branchRestackCmd `cmd:"" aliases:"rs" help:"Restack a branch on its base"`
	Squash  branchSquashCmd  `cmd:"" aliases:"sq" help:"Squash the commits of a branch into a single commit"`

	// Pull request management
	Submit branchSubmitCmd `cmd:"" aliases:"s" help:"Submit the current branch"`
}

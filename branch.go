package main

type branchCmd struct {
	Track    branchTrackCmd    `cmd:"" aliases:"tr" help:"Begin tracking a branch with gs"`
	Untrack  branchUntrackCmd  `cmd:"" aliases:"utr" help:"Stop tracking a branch with gs"`
	Checkout branchCheckoutCmd `cmd:"" aliases:"co" help:"Checkout a specific branch"`

	// Creation and destruction
	Create branchCreateCmd `cmd:"" aliases:"c" help:"Create a new branch"`
	Delete branchDeleteCmd `cmd:"" aliases:"rm" help:"Delete the current branch"`
	Fold   branchFoldCmd   `cmd:"" aliases:"f" help:"Fold a branch into its base"`

	// Mutation
	Edit    branchEditCmd    `cmd:"" aliases:"e" help:"Edit the current branch"`
	Rename  branchRenameCmd  `cmd:"" aliases:"mv" help:"Rename the current branch"`
	Restack branchRestackCmd `cmd:"" aliases:"rs" help:"Restack just one branch"`
}

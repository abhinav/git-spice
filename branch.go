package main

type branchCmd struct {
	Track   branchTrackCmd   `cmd:"" aliases:"tr" help:"Begin tracking a branch with gs"`
	Untrack branchUntrackCmd `cmd:"" aliases:"untr" help:"Stop tracking a branch with gs"`

	// Creation and destruction
	Create branchCreateCmd `cmd:"" aliases:"c" help:"Create a new branch"`
	Delete branchDeleteCmd `cmd:"" aliases:"rm" help:"Delete the current branch"`
	Fold   branchFoldCmd   `cmd:"" help:"Fold a branch into its base"`

	// Mutation
	Edit    branchEditCmd    `cmd:"" aliases:"e" help:"Edit the current branch"`
	Rename  branchRenameCmd  `cmd:"" aliases:"mv" help:"Rename the current branch"`
	Restack branchRestackCmd `cmd:"" aliases:"rs" help:"Restack just one branch"`

	// Navigation
	Up       branchUpCmd       `cmd:"" aliases:"u" group:"Navigation" help:"Move up the stack"`
	Down     branchDownCmd     `cmd:"" aliases:"d" group:"Navigation" help:"Move down the stack"`
	Top      branchTopCmd      `cmd:"" aliases:"t" group:"Navigation" help:"Move to the top of the stack"`
	Bottom   branchBottomCmd   `cmd:"" aliases:"b" group:"Navigation" help:"Move to the bottom of the stack"`
	Checkout branchCheckoutCmd `cmd:"" aliases:"co" group:"Navigation" help:"Checkout a specific branch"`
}

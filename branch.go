package main

type branchCmd struct {
	Track   branchTrackCmd   `cmd:"" aliases:"tr" help:"Begin tracking a branch with gs"`
	Untrack branchUntrackCmd `cmd:"" aliases:"utr" help:"Stop tracking a branch with gs"`

	// Creation and destruction
	Delete branchDeleteCmd `cmd:"" aliases:"de" help:"Delete the current branch"`

	// Mutation
	Edit   branchEditCmd   `cmd:"" aliases:"e" help:"Edit the current branch"`
	Rename branchRenameCmd `cmd:"" aliases:"r" help:"Rename the current branch"`

	// Relative movements
	Up     branchUpCmd     `cmd:"" aliases:"u" help:"Move up the stack"`
	Down   branchDownCmd   `cmd:"" aliases:"d" help:"Move down the stack"`
	Top    branchTopCmd    `cmd:"" aliases:"t" help:"Move to the top of the stack"`
	Bottom branchBottomCmd `cmd:"" aliases:"b" help:"Move to the bottom of the stack"`

	// Absolute movements
	Checkout branchCheckoutCmd `cmd:"" aliases:"co" help:"Checkout a specific pull request"`
}

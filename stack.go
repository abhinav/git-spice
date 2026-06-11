package main

type stackCmd struct {
	Submit  stackSubmitCmd  `cmd:"" aliases:"s" help:"Submit a stack"`
	Sync    stackSyncCmd    `cmd:"" aliases:"sy" help:"Pull remote-side commits for every branch in the stack" released:"unreleased"`
	Restack stackRestackCmd `cmd:"" aliases:"r" help:"Restack a stack"`
	Edit    stackEditCmd    `cmd:"" aliases:"e" help:"Edit the order of branches in a stack"`
	Delete  stackDeleteCmd  `cmd:"" aliases:"d" released:"v0.16.0" help:"Delete all branches in a stack"`
}

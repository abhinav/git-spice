package main

type stackCmd struct {
	Submit  stackSubmitCmd  `cmd:"" aliases:"s" help:"Submit a stack"`
	Restack stackRestackCmd `cmd:"" aliases:"r" help:"Restack a stack"`
	Edit    stackEditCmd    `cmd:"" aliases:"e" help:"Edit the order of branches in a stack"`
	Delete  stackDeleteCmd  `cmd:"" aliases:"d" help:"Delete all branches in a stack"`
}

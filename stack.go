package main

type stackCmd struct {
	Submit  stackSubmitCmd  `cmd:"" aliases:"s" help:"Submit the current stack"`
	Edit    stackEditCmd    `cmd:"" aliases:"e" help:"Edit the order of branches in the current stack"`
	Restack stackRestackCmd `cmd:"" aliases:"r" help:"Restack the current stack"`
}

package main

type stackCmd struct {
	Submit  stackSubmitCmd  `cmd:"" aliases:"s" help:"Submit the current stack"`
	Restack stackRestackCmd `cmd:"" aliases:"r" help:"Restack the current stack"`
}

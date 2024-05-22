package gitspice

type stackCmd struct {
	Submit  stackSubmitCmd  `cmd:"" aliases:"s" help:"Submit the current stack"`
	Restack stackRestackCmd `cmd:"" aliases:"rs" help:"Restack the current stack"`
}

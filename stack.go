package main

type stackCmd struct {
	Submit stackSubmitCmd `cmd:"" aliases:"s" help:"Submit the current stack"`
}

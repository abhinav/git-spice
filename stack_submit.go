package main

type stackSubmitCmd struct {
	DryRun bool `short:"n" help:"Don't actually submit the stack"`
}

func (cmd *stackSubmitCmd) Run() error {
	panic("TODO")
}

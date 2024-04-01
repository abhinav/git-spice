package main

type downstackSubmitCmd struct {
	DryRun bool `short:"n" help:"Don't actually submit the stack"`
}

func (*downstackSubmitCmd) Run() error { panic("TODO") }

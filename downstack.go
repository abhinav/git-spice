package main

type downstackCmd struct {
	Submit downstackSubmitCmd `cmd:"" aliases:"s" help:"Submit the current branch and those below it"`
	Edit   downstackEditCmd   `cmd:"" aliases:"e" help:"Edit the order of branches below the current branch"`
}

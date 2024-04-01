package main

type downstackCmd struct {
	Submit downstackSubmitCmd `cmd:"" aliases:"s" help:"Submit branches below the current branch"`
	Edit   downstackEditCmd   `cmd:"" aliases:"e" help:"Edit the order of branches below the current branch"`
}

package main

type downstackCmd struct {
	Edit downstackEditCmd `cmd:"" aliases:"e" help:"Edit the order of branches below the current branch"`
}

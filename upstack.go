package main

type upstackCmd struct {
	Onto    upstackOntoCmd    `cmd:"" aliases:"o" help:"Move upstack of a branch onto a different branch"`
	Restack upstackRestackCmd `cmd:"" aliases:"rs" help:"Restack upstack branches"`
}

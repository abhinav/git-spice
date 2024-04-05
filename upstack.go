package main

type upstackCmd struct {
	Restack upstackRestackCmd `cmd:"" aliases:"rs" help:"Restack upstack branches"`
}

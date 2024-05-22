package main

type upstackCmd struct {
	Restack upstackRestackCmd `cmd:"" aliases:"rs" help:"Restack this branch those above it"`
}

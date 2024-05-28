package main

type upstackCmd struct {
	Restack upstackRestackCmd `cmd:"" aliases:"r" help:"Restack this branch those above it"`
	Onto    upstackOntoCmd    `cmd:"" aliases:"o" help:"Move this branch onto another branch"`
}

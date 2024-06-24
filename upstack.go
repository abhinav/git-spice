package main

type upstackCmd struct {
	Restack upstackRestackCmd `cmd:"" aliases:"r" help:"Restack a branch and its upstack"`
	Onto    upstackOntoCmd    `cmd:"" aliases:"o" help:"Move a branch onto another branch"`
}

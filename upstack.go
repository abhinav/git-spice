package main

type upstackCmd struct {
	Submit  upstackSubmitCmd  `cmd:"" aliases:"s" help:"Submit a branch and those above it"`
	Restack upstackRestackCmd `cmd:"" aliases:"r" help:"Restack a branch and its upstack"`
	Onto    upstackOntoCmd    `cmd:"" aliases:"o" help:"Move a branch onto another branch"`
}

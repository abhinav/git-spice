package main

type repoCmd struct {
	Init    repoInitCmd    `cmd:"" aliases:"i" help:"Initialize a repository"`
	Sync    repoSyncCmd    `cmd:"" aliases:"s" help:"Pull latest changes from the remote"`
	Restack repoRestackCmd `cmd:"" aliases:"r" help:"Restack all tracked branches" released:"unreleased"`
}

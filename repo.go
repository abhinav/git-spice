package main

type repoCmd struct {
	Init repoInitCmd `cmd:"" aliases:"i" help:"Initialize a repository"`
}

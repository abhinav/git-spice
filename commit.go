package main

type commitCmd struct {
	Create commitCreateCmd `cmd:"" aliases:"c" help:"Create a new commit"`
	Amend  commitAmendCmd  `cmd:"" aliases:"a" help:"Amend the current commit"`
	Pick   commitPickCmd   `cmd:"" aliases:"p" help:"Cherry-pick a commit"`
	Split  commitSplitCmd  `cmd:"" aliases:"sp" help:"Split the current commit"`
}

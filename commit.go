package gitspice

type commitCmd struct {
	Create commitCreateCmd `cmd:"" aliases:"c" help:"Create a new commit"`
	Amend  commitAmendCmd  `cmd:"" aliases:"a" help:"Amend the current commit"`
}

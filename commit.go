package main

type commitCmd struct {
	Create commitCreateCmd `cmd:"" aliases:"c" help:"Create a new commit"`
	Amend  commitAmendCmd  `cmd:"" aliases:"a" help:"Amend the current commit"`
	Split  commitSplitCmd  `cmd:"" aliases:"sp" help:"Split the current commit"`

	Fixup commitFixupCmd `cmd:"" aliases:"f" experiment:"commitFixup" help:"Fixup a commit below the current commit"`
	// TODO: When fixup is stabilized, add a 'released:' tag here.
}

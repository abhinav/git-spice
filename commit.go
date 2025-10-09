package main

import "go.abhg.dev/gs/internal/git"

type commitOptions struct {
	Message  string `short:"m" placeholder:"MSG" help:"Use the given message as the commit message."`
	NoVerify bool   `help:"Bypass pre-commit and commit-msg hooks."`
}

func (opts *commitOptions) commitRequest(req *git.CommitRequest) git.CommitRequest {
	if req == nil {
		req = &git.CommitRequest{}
	}
	req.Message = opts.Message
	req.NoVerify = opts.NoVerify
	return *req
}

type commitCmd struct {
	Create commitCreateCmd `cmd:"" aliases:"c" help:"Create a new commit"`
	Amend  commitAmendCmd  `cmd:"" aliases:"a" help:"Amend the current commit"`
	Split  commitSplitCmd  `cmd:"" aliases:"sp" help:"Split the current commit"`

	Fixup commitFixupCmd `cmd:"" aliases:"f" experiment:"commitFixup" help:"Fixup a commit below the current commit"`
	// TODO: When fixup is stabilized, add a 'released:' tag here.
}

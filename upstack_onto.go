package main

type upstackOntoCmd struct {
	Branch string `arg:"" optional:"" help:"Branch to rebase the stack onto"`
}

func (*upstackOntoCmd) Help() string {
	return "Changes the base of a branch, restacking all branches above it. " +
		"Use this to graft part of a branch tree to a different base branch."
}

func (*upstackOntoCmd) Run() error { panic("TODO") }

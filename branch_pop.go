package main

type branchPopCmd struct{}

func (*branchPopCmd) Run() error {
	// TODO git reset --soft parent,
	// but only if there's no upstack and we're fully restacked
	panic("TODO")
}

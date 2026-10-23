package main

type rebaseCmd struct {
	Continue rebaseContinueCmd `aliases:"c" cmd:"" help:"Continue an interrupted operation (rebase or merge)"`
	Abort    rebaseAbortCmd    `aliases:"a" cmd:"" help:"Abort an interrupted operation (rebase or merge)"`
}

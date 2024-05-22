package main

type rebaseCmd struct {
	Continue rebaseContinueCmd `aliases:"c" cmd:"" help:"Continue an interrupted operation"`
	Abort    rebaseAbortCmd    `aliases:"a" cmd:"" help:"Abort an operation"`
}

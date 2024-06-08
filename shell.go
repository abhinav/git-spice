package main

type shellCmd struct {
	Completion shellCompletionCmd `cmd:"" help:"Generate shell completion script"`
}

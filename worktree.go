package main

type worktreeCmd struct {
	List   worktreeListCmd   `cmd:"" aliases:"ls" help:"List worktrees and their branches"`
	Create worktreeCreateCmd `cmd:"" aliases:"c" help:"Create a new worktree"`
}

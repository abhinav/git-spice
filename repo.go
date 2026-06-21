package main

type repoCmd struct {
	Init    repoInitCmd    `cmd:"" aliases:"i" help:"Initialize a repository"`
	Sync    repoSyncCmd    `cmd:"" aliases:"s" help:"Pull latest changes from the remote"`
	Restack repoRestackCmd `cmd:"" aliases:"r" help:"Restack all tracked branches" released:"v0.16.0"`

	Park      repoParkCmd      `cmd:"" help:"Enter exclusive mode: park worktrees and take the whole repo"`
	Restore   repoRestoreCmd   `cmd:"" help:"Leave exclusive mode: restore parked worktrees"`
	Exclusive repoExclusiveCmd `cmd:"" help:"Run a command with the whole repo to itself"`
}

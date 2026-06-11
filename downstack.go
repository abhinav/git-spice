package main

type downstackCmd struct {
	Track   downstackTrackCmd   `cmd:"" aliases:"tr" help:"Track all untracked branches below a branch"`
	Submit  downstackSubmitCmd  `cmd:"" aliases:"s" help:"Submit a branch and those below it"`
	Sync    downstackSyncCmd    `cmd:"" aliases:"sy" help:"Pull remote-side commits for a branch and those below it" released:"unreleased"`
	Merge   downstackMergeCmd   `cmd:"" aliases:"m" experiment:"merge" help:"Merge a branch and those below it"`
	Edit    downstackEditCmd    `cmd:"" aliases:"e" help:"Edit the order of branches below a branch"`
	Restack downstackRestackCmd `cmd:"" aliases:"r" help:"Restack a branch and its downstack"`
}

package main

// ciCmd groups subcommands for CI/CD integration.
type ciCmd struct {
	MergeGuard ciMergeGuardCmd `cmd:"merge-guard" help:"Block merging a PR whose base is not trunk"`
}

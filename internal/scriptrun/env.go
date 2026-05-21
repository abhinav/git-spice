package scriptrun

// Operation names the high-level gs command driving a script call.
// Used as the value of GS_OPERATION in the script's environment so
// scripts can branch on which gs subcommand invoked them.
//
// See doc/src/guide/scripts.md for the full table.
type Operation string

// Shared GS_OPERATION values. Keep in sync with doc/src/guide/scripts.md.
const (
	OpCommitCreate       Operation = "commit-create"
	OpCommitAmend        Operation = "commit-amend"
	OpBranchCreate       Operation = "branch-create"
	OpBranchSubmit       Operation = "branch-submit"
	OpBranchSquash       Operation = "branch-squash"
	OpBranchRestack      Operation = "branch-restack"
	OpUpstackRestack     Operation = "upstack-restack"
	OpDownstackRestack   Operation = "downstack-restack"
	OpStackRestack       Operation = "stack-restack"
	OpRepoRestack        Operation = "repo-restack"
	OpIntegrationRebuild Operation = "integration-rebuild"
)

// EnvFor returns the shared minimum environment that every script
// receives: GS_OPERATION (required), GS_BRANCH and GS_BASE (omitted
// when empty). Feature-specific extras layer on top of this slice via
// the existing RunRequest.Env mechanism.
//
// The returned slice is safe to append to.
func EnvFor(op Operation, branch, base string) []string {
	env := make([]string, 0, 3)
	env = append(env, "GS_OPERATION="+string(op))
	if branch != "" {
		env = append(env, "GS_BRANCH="+branch)
	}
	if base != "" {
		env = append(env, "GS_BASE="+base)
	}
	return env
}

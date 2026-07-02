package main

const _mergeHelpCommon = `

Branches merge bottom-up starting with those stacked on trunk.
After a branch merges, its upstack branches are restacked and resubmitted.
When those are ready to merge, they are merged in turn, and the process repeats.

A branch is considered ready to merge when the forge reports it as mergeable,
based on the forge and the repository configuration.
Override this with the 'spice.merge.readyCommand' configuration option.

Branches are merged using the forge's merge API.
Override this with the 'spice.merge.command' configuration option.

If a branch becomes blocked and will not become ready without intervention,
or it takes too long to become ready, or otherwise fails to merge,
it is skipped and any branches stacked on it are also skipped.
Use --fail-fast to stop scheduling remaining merge queue work
after the first branch failure.
`

package git

// SetConflictStyle exports test-only functionality
// for external tests to set the conflict marker style
// for a merge-tree operation.
func SetConflictStyle(req *MergeTreeRequest, style string) {
	req.conflictStyle = style
}

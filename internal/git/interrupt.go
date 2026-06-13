package git

// InterruptError is the common interface implemented by errors
// that report an interrupted Git operation
// (e.g. a rebase or merge stopped by a conflict).
type InterruptError interface {
	error

	// InterruptedBranch reports the branch
	// on which the operation was interrupted.
	InterruptedBranch() string

	interruptError()
}

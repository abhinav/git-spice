package git

import (
	"bufio"
	"context"
	"errors"
)

// RevList iterates over the commits in a repository.
//
// Use this like bufio.Scanner:
//
//	for revList.Next() {
//		commit := revList.Commit()
//		// ...
//	}
//	if err := revList.Err(); err != nil {
//		// ...
//	}
type RevList struct {
	cmd  *gitCmd
	out  *bufio.Scanner
	err  error
	exec execer
}

// Next reports whether there is another commit in the list.
func (r *RevList) Next() bool {
	if r.out.Scan() {
		return true
	}

	if err := r.out.Err(); err != nil {
		// Reading output failed.
		// Kill the command.
		r.err = r.cmd.Kill(r.exec)
		return false
	}

	// Reached EOF.
	// Wait for the command to exit.
	r.err = r.cmd.Wait(r.exec)
	return false
}

// Commit returns the commit at the current position.
// Next must have been called before this.
func (r *RevList) Commit() string {
	return r.out.Text()
}

// Err returns errors encountered while iterating
// or waiting for the command to exit.
func (r *RevList) Err() error {
	return errors.Join(r.err, r.out.Err())
}

// ListCommits returns a list of commits in the range [start, stop).
func (r *Repository) ListCommits(ctx context.Context, start, stop string) (*RevList, error) {
	cmd := r.gitCmd(ctx, "rev-list", start, "--not", stop)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &RevList{
		cmd:  cmd,
		out:  bufio.NewScanner(out),
		exec: r.exec,
	}, nil
}

package git

import "context"

// Notes accesses the Git notes associated with a repository.
type Notes struct {
	r    *Repository
	ref  string
	exec execer
}

// Notes returns a Notes instance for the given ref.
// If ref is empty, the default ref "refs/notes/commits" is used.
func (r *Repository) Notes(ref string) *Notes {
	if ref == "" {
		ref = "refs/notes/commits"
	}

	return &Notes{
		r:    r,
		ref:  ref,
		exec: r.exec,
	}
}

// AddNoteOptions configures the behavior of Notes.Add.
type AddNoteOptions struct {
	// Force indicates whether to overwrite an existing note.
	// If false, an error will be returned if a note already exists.
	Force bool
}

// Add adds note msg to object obj.
//
// Fails if a note already exists.
// Overwrite with opts.Force.
func (n *Notes) Add(ctx context.Context, obj, msg string, opts *AddNoteOptions) error {
	if opts == nil {
		opts = &AddNoteOptions{}
	}

	args := make([]string, 0, 8)
	args = append(args, "notes", "--ref", n.ref)
	args = append(args, "add")
	if opts.Force {
		args = append(args, "-f")
	}
	args = append(args, "-m", msg, obj)
	return n.r.gitCmd(ctx, args...).Run(n.exec)
}

// Show returns the contents of the note associated with obj, if any.
func (n *Notes) Show(ctx context.Context, obj string) (string, error) {
	return n.r.gitCmd(ctx, "notes", "--ref", n.ref, "show", obj).OutputString(n.exec)
}

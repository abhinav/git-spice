package git

import (
	"context"
	"fmt"
	"io"

	"go.abhg.dev/gs/internal/must"
)

// Type specifies the type of a Git object.
type Type string

// Supported object types.
const (
	BlobType   Type = "blob"
	CommitType Type = "commit"
	TreeType   Type = "tree"
)

func (t Type) String() string {
	return string(t)
}

// ReadObject reads the object with the given hash from the repository
// into the given writer.
//
// This is not useful for tree objects. Use ListTree instead.
func (r *Repository) ReadObject(ctx context.Context, typ Type, hash Hash, dst io.Writer) error {
	must.NotBeBlankf(string(typ), "object type must not be blank")
	must.NotBeBlankf(string(hash), "object hash must not be blank")

	cmd := r.gitCmd(ctx, "cat-file", string(typ), hash.String()).WithStdout(dst)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cat-file: %w", err)
	}
	return nil
}

// WriteObject writes an object of the given type to the repository,
// and returns the hash of the written object.
func (r *Repository) WriteObject(ctx context.Context, typ Type, src io.Reader) (Hash, error) {
	must.NotBeBlankf(string(typ), "object type must not be blank")

	cmd := r.gitCmd(ctx, "hash-object", "-w", "--stdin", "-t", string(typ)).WithStdin(src)
	out, err := cmd.OutputChomp()
	if err != nil {
		return ZeroHash, fmt.Errorf("hash-object: %w", err)
	}
	return Hash(out), nil
}

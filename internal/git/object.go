package git

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

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

// ReadObjects reads the objects with the given hashes from the repository.
//
// The returned reader yields objects in the same order as hashes.
// Callers must close the reader after use.
//
// [ObjectBatchReader.Next] skips unread bytes from the current object before
// advancing to the next object.
func (r *Repository) ReadObjects(
	ctx context.Context,
	hashes []Hash,
) (*ObjectBatchReader, error) {
	var stdin strings.Builder
	for _, hash := range hashes {
		stdin.WriteString(hash.String())
		stdin.WriteByte('\n')
	}

	// Git 2.38 is still part of the test matrix,
	// so this uses the LF-delimited batch protocol instead of -Z.
	//
	// The request is one object name per line on stdin.
	// For each object, stdout returns:
	//
	//	<object> SP <type> SP <size> LF
	//	<size> bytes of object contents
	//	LF
	//
	// Missing objects return:
	//
	//	<object> SP missing LF
	cmd := r.gitCmd(ctx, "cat-file", "--batch").
		WithStdinString(stdin.String())

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("cat-file stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cat-file: %w", err)
	}

	return newObjectBatchReader(cmd, stdout), nil
}

// ObjectInfo describes one Git object in an [ObjectBatchReader].
type ObjectInfo struct {
	// ID is the full object ID.
	ID Hash

	// Type is the Git object type.
	Type Type

	// Size is the object body size in bytes.
	Size int64
}

// ObjectBatchReader reads objects returned by [Repository.ReadObjects].
//
// Use [ObjectBatchReader.Next] to advance to an object and [ObjectBatchReader.Read]
// to read that object's body. After Read returns [io.EOF], call Next again to
// advance to the next object.
//
// ObjectBatchReader is not safe for concurrent use.
type ObjectBatchReader struct {
	cmd    *gitCmd
	stdout io.Closer

	// r reads cat-file headers, object contents, and object terminators
	// from the same ordered byte stream.
	r *bufio.Reader

	current *ObjectInfo

	// remaining counts unread object body bytes.
	// It does not include the LF terminator that cat-file emits
	// after each body.
	remaining int64

	// done records whether the body terminator has been consumed.
	// It is separate from remaining because remaining reaches zero
	// before the terminator is read.
	done bool

	exhausted bool
	closed    bool
}

func newObjectBatchReader(cmd *gitCmd, stdout io.ReadCloser) *ObjectBatchReader {
	return &ObjectBatchReader{
		cmd:    cmd,
		stdout: stdout,
		r:      bufio.NewReader(stdout),
	}
}

// Next advances to the next object in the batch.
//
// If the current object body was not fully read,
// Next discards its unread bytes before reading the next header.
// It returns [io.EOF] after the last object.
// It returns an error matching [ErrNotExist] if Git reports a missing object.
func (r *ObjectBatchReader) Next() (*ObjectInfo, error) {
	if r.current != nil {
		if err := r.discard(); err != nil {
			return nil, err
		}
	}

	header, err := r.r.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && header == "" {
			r.exhausted = true
			return nil, io.EOF
		}
		return nil, fmt.Errorf("read cat-file header: %w", err)
	}

	fields := strings.Fields(strings.TrimSuffix(header, "\n"))
	if len(fields) == 2 && fields[1] == "missing" {
		return nil, fmt.Errorf("read object %s: %w", fields[0], ErrNotExist)
	}
	if len(fields) != 3 {
		return nil, fmt.Errorf("parse cat-file header %q", strings.TrimSpace(header))
	}

	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse object size: %w", err)
	}

	r.current = &ObjectInfo{
		ID:   Hash(fields[0]),
		Type: Type(fields[1]),
		Size: size,
	}
	r.remaining = size
	r.done = false
	return r.current, nil
}

// Read reads the current object body.
//
// Read returns [io.EOF] after the full body has been read and the cat-file body
// terminator has been consumed. Call [ObjectBatchReader.Next] again to advance
// to the next object.
func (r *ObjectBatchReader) Read(p []byte) (int, error) {
	if r.current == nil {
		return 0, io.EOF
	}
	if r.remaining == 0 {
		return 0, r.finish()
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	if err != nil {
		return n, fmt.Errorf("read object body: %w", err)
	}
	if r.remaining == 0 {
		return n, r.finish()
	}
	return n, nil
}

// Close releases the Git process and stdout pipe owned by the reader.
//
// Callers should close every ObjectBatchReader returned by
// [Repository.ReadObjects]. If the batch was fully consumed, Close reports
// cat-file exit errors. If the batch was not fully consumed, Close terminates
// cat-file and does not report the expected early termination.
func (r *ObjectBatchReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true

	if r.exhausted {
		if err := r.cmd.Wait(); err != nil {
			return fmt.Errorf("cat-file: %w", err)
		}
		return nil
	}

	_ = r.stdout.Close()
	_ = r.cmd.Kill()
	_ = r.cmd.Wait()
	return nil
}

func (r *ObjectBatchReader) discard() error {
	if r.done {
		return nil
	}
	if _, err := io.CopyN(io.Discard, r.r, r.remaining); err != nil {
		return fmt.Errorf("discard object body: %w", err)
	}
	r.remaining = 0
	if err := r.finish(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (r *ObjectBatchReader) finish() error {
	if r.done {
		return io.EOF
	}
	linefeed, err := r.r.ReadByte()
	if err != nil {
		return fmt.Errorf("read object terminator: %w", err)
	}
	if linefeed != '\n' {
		return fmt.Errorf("read object terminator: got %q", linefeed)
	}
	r.done = true
	return io.EOF
}

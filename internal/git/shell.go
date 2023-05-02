package git

import (
	"bufio"
	"context"
	"log"
	"os/exec"

	"go.abhg.dev/git-stack/internal/ioutil"
	"go.abhg.dev/git-stack/internal/syncutil"
)

type commander func(context.Context, string, ...string) *exec.Cmd

// Shell implements [Git] by shelling out to the Git CLI.
type Shell struct {
	// Logger is the logger to use for Git output.
	Logger *log.Logger // optional

	// WorkDir is the working directory to use for Git commands.
	//
	// If empty, the current working directory will be used.
	WorkDir string

	commander syncutil.SetOnce[commander]
}

var _ Git = (*Shell)(nil)

func (s *Shell) gitCmd(ctx context.Context, args ...string) (cmd *exec.Cmd, done func()) {
	newCommand := s.commander.Get(exec.CommandContext)
	cmd = newCommand(ctx, "git", args...)
	cmd.Dir = s.WorkDir
	cmd.Stderr, done = ioutil.LogWriter(s.Logger, "[git] ")
	return cmd, done
}

// AddNote adds a Git note to an object.
func (s *Shell) AddNote(ctx context.Context, req AddNoteRequest) error {
	args := make([]string, 0, 8)
	args = append(args, "notes")
	if req.Ref != "" {
		args = append(args, "--ref", req.Ref)
	}
	args = append(args, "add")
	if req.Force {
		args = append(args, "-f")
	}
	args = append(args, "-m", req.Message, req.Object)

	cmd, done := s.gitCmd(ctx, args...)
	defer done()
	return cmd.Run()
}

// ListCommits lists matching commits.
func (s *Shell) ListCommits(ctx context.Context, req ListCommitsRequest) ([]string, error) {
	args := make([]string, 0, 4)
	args = append(args, "rev-list")
	if req.Start != "" {
		args = append(args, req.Start)
	}
	if req.Stop != "" {
		args = append(args, "--not", req.Stop)
	}

	cmd, done := s.gitCmd(ctx, args...)
	defer done()

	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var commits []string
	r := bufio.NewScanner(out)
	for r.Scan() {
		commits = append(commits, r.Text())
	}
	if err := r.Err(); err != nil {
		return nil, err
	}

	return commits, cmd.Wait()
}

package git

import (
	"context"
	"log"
	"os/exec"

	"go.abhg.dev/git-stack/internal/logx"
	"go.abhg.dev/git-stack/internal/syncx"
)

type commander func(context.Context, string, ...string) *exec.Cmd

type Shell struct {
	Logger  *log.Logger
	WorkDir string

	commander syncx.SetOnce[commander]
}

var _ Git = (*Shell)(nil)

func (s *Shell) gitCmd(ctx context.Context, args ...string) (cmd *exec.Cmd, done func()) {
	newCommand := s.commander.Get(exec.CommandContext)
	cmd = newCommand(ctx, "git", args...)
	cmd.Dir = s.WorkDir
	cmd.Stderr, done = logx.Writer(s.Logger, "[git] ")
	return cmd, done
}

func (s *Shell) AddNote(ctx context.Context, req *AddNoteRequest) error {
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

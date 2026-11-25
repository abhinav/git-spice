package msggen

import (
	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/scriptrun"
)

// Options selects automatic message filling.
type Options struct {
	Fill    bool   `negatable:"" config:"message.fill" help:"Fill the message automatically."`
	Command string `hidden:"" config:"message.fill.command" help:"Command that edits the message file in place."`
}

// AfterApply binds message filling after flags and configuration are resolved.
func (o *Options) AfterApply(ctx *kong.Context) error {
	ctx.Bind(&CommitEditor{Fill: o.Fill, Command: o.Command})
	return ctx.BindSingletonProvider(func(wt *git.Worktree, runner *scriptrun.Runner) *Handler {
		return &Handler{
			Fill:    o.Fill,
			Command: o.Command,
			Dir:     wt.RootDir(),
			Runner:  runner,
		}
	})
}

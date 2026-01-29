package main

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/alecthomas/kong"
)

type versionFlag bool

func (v versionFlag) BeforeReset(app *kong.Kong) error {
	if err := new(versionCmd).Run(app); err != nil {
		return err
	}

	app.Exit(0)
	return nil
}

type versionCmd struct {
	Short bool `help:"Print only the version number."`
}

func (cmd *versionCmd) Run(app *kong.Kong) error {
	if cmd.Short {
		fmt.Fprintln(app.Stdout, _version)
		return nil
	}

	fmt.Fprint(app.Stdout, "git-spice ", _version)
	if report := _generateBuildReport(); report != "" {
		fmt.Fprintf(app.Stdout, " (%s)", report)
	}

	fmt.Fprintln(app.Stdout)
	fmt.Fprintln(app.Stdout, "Copyright (C) vvanirudh")
	fmt.Fprintln(app.Stdout, "  <https://github.com/vvanirudh/gritt-spice>")
	fmt.Fprintln(app.Stdout, "This program comes with ABSOLUTELY NO WARRANTY")
	fmt.Fprintln(app.Stdout, "This is free software, and you are welcome to redistribute it")
	fmt.Fprintln(app.Stdout, "under certain conditions; see source for details.")

	return nil
}

var _debugReadBuildInfo = debug.ReadBuildInfo

var _generateBuildReport = func() string {
	info, ok := _debugReadBuildInfo()
	if !ok {
		return ""
	}

	var (
		revision string
		dirty    bool
		time     string
	)
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		case "vcs.time":
			time = setting.Value
		}
	}

	var out strings.Builder
	if revision != "" {
		out.WriteString(revision)
		if dirty {
			out.WriteString("-dirty")
		}
	}
	if time != "" {
		if out.Len() > 0 {
			fmt.Fprint(&out, " ")
		}
		out.WriteString(time)
	}

	return out.String()
}

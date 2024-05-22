package main

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/alecthomas/kong"
)

type versionFlag bool

func (v versionFlag) BeforeReset(app *kong.Kong) error {
	fmt.Fprint(app.Stdout, "git-spice ", _version)
	if report := generateBuildReport(); report != "" {
		fmt.Fprintf(app.Stdout, " (%s)", report)
	}

	fmt.Fprintln(app.Stdout)
	fmt.Fprintln(app.Stdout, "Copyright (C) 2024 Abhinav Gupta")
	fmt.Fprintln(app.Stdout, "  <https://github.com/abhinav/git-spice>")
	fmt.Fprintln(app.Stdout, "This program comes with ABSOLUTELY NO WARRANTY")
	fmt.Fprintln(app.Stdout, "This is free software, and you are welcome to redistribute it")
	fmt.Fprintln(app.Stdout, "under certain conditions; see source for details.")
	app.Exit(0)
	return nil
}

func generateBuildReport() string {
	info, ok := debug.ReadBuildInfo()
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

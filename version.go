package main

import (
	"fmt"

	"github.com/alecthomas/kong"
)

var _version = "dev"

type versionCmd struct{}

func (cmd *versionCmd) Run(app *kong.Kong) error {
	fmt.Fprintln(app.Stdout, "git-stack", _version)
	fmt.Fprintln(app.Stdout, "Copyright (C) 2023 Abhinav Gupta")
	fmt.Fprintln(app.Stdout, "  <https://github.com/abhinav/git-stack>")
	fmt.Fprintln(app.Stdout, "This program comes with ABSOLUTELY NO WARRANTY")
	fmt.Fprintln(app.Stdout, "This is free software, and you are welcome to redistribute it")
	fmt.Fprintln(app.Stdout, "under certain conditions; see source for details.")
	app.Exit(0)
	return nil
}

type versionFlag bool

func (v versionFlag) BeforeReset(app *kong.Kong) error {
	return (*versionCmd)(nil).Run(app)
}

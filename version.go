package main

import (
	"fmt"

	"github.com/alecthomas/kong"
)

type versionFlag bool

func (v versionFlag) BeforeReset(app *kong.Kong) error {
	fmt.Fprintln(app.Stdout, "gs", _version)
	fmt.Fprintln(app.Stdout, "Copyright (C) 2024 Abhinav Gupta")
	fmt.Fprintln(app.Stdout, "  <https://github.com/abhinav/gs>")
	fmt.Fprintln(app.Stdout, "This program comes with ABSOLUTELY NO WARRANTY")
	fmt.Fprintln(app.Stdout, "This is free software, and you are welcome to redistribute it")
	fmt.Fprintln(app.Stdout, "under certain conditions; see source for details.")
	app.Exit(0)
	return nil
}

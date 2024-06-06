package main

type logCmd struct {
	Short logShortCmd `cmd:"" aliases:"s" help:"Short view of stack"`
}

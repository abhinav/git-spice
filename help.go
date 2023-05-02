package main

import (
	_ "embed"
	"strings"

	"github.com/peterbourgon/ff/v3/ffcli"
)

var (
	//go:embed help/main.txt
	_mainUsage string

	//go:embed help/submit.txt
	_submitUsage string
)

func usageText(s string) func(*ffcli.Command) string {
	return func(*ffcli.Command) string {
		return strings.TrimSpace(s)
	}
}

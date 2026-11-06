package main

import "flag"

func init() {
	if flag.Lookup("update") == nil {
		flag.Bool("update", false, "update golden files")
	}
}

func updateFlag() bool {
	return flag.Lookup("update").Value.(flag.Getter).Get().(bool)
}

package ui

import (
	"flag"
	"strconv"
)

func init() {
	if flag.Lookup("update") == nil {
		flag.Bool("update", false, "update test fixtures")
	}
}

// UpdateFixtures reports whether tests should update UI fixtures.
//
// It shares an existing -update flag if another test helper registered one.
func UpdateFixtures() bool {
	f := flag.Lookup("update")
	if f == nil {
		return false
	}
	update, _ := strconv.ParseBool(f.Value.String())
	return update
}

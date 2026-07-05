package forgetest

import "flag"

// Update returns true if running in fixture update mode.
func Update() bool {
	return flag.Lookup("update").Value.(flag.Getter).Get().(bool)
}

func init() {
	if flag.Lookup("update") == nil {
		flag.Bool("update", false, "update test fixtures")
	}
}

package github

import "flag"

var UpdateFixtures *bool

func init() {
	if updateFlag := flag.Lookup("update"); updateFlag != nil {
		value := updateFlag.Value.(flag.Getter).Get().(bool)
		UpdateFixtures = &value
	} else {
		UpdateFixtures = flag.Bool("update", false, "update test fixtures")
	}
}

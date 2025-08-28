package gitlab

import "flag"

func UpdateFixtures() bool {
	return flag.Lookup("update").Value.(flag.Getter).Get().(bool)
}

func init() {
	if flag.Lookup("update") == nil {
		flag.Bool("update", false, "update test fixtures")
	}
}

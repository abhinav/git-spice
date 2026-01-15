package widget

import (
	"os"
	"time"
)

var _timeNow = time.Now

func init() {
	now := os.Getenv("GIT_SPICE_NOW")
	if now != "" {
		t, err := time.Parse(time.RFC3339, now)
		if err == nil {
			_timeNow = func() time.Time {
				return t
			}
		}
	}
}

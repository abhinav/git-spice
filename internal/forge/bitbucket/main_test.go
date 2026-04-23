package bitbucket

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	name := filepath.Base(os.Args[0])
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	}

	if name == "git" {
		if len(os.Args) == 3 && os.Args[1] == "credential" && os.Args[2] == "fill" {
			fmt.Print(`protocol=https
host=bitbucket.org
username=test-user
password=test-token
`)
			os.Exit(0)
		}

		os.Exit(1)
	}

	os.Exit(m.Run())
}

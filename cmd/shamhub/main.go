// Command shamhub controls a ShamHub server.
package main

import (
	"os"

	"go.abhg.dev/gs/internal/forge/shamhub"
)

func main() {
	os.Exit(shamhub.CLI())
}

// test-matrix generates the matrix for the "test" job in the CI pipeline.
package main

import (
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

var _indent = flag.Bool("indent", false, "indent JSON output")

type testConfig struct {
	// GitVersion is the list of Git versions
	// against which this test job should run.
	OS string

	// GitVersions is the list of Git versions
	// against which this test job should run.
	GitVersions []string

	// ScriptShards is the number of shards to use for script tests.
	ScriptShards int

	// Whether to run with race detector and coverage.
	Race, Cover bool
}

type testSuite string

const (
	defaultTests testSuite = "default"
	scriptTests  testSuite = "script"
)

type matrixEntry struct {
	Name string `json:"name"`
	OS   string `json:"os"`

	GitVersion string    `json:"git-version"`
	Suite      testSuite `json:"suite"`

	Race  bool `json:"race"`
	Cover bool `json:"cover"`

	// ShardIndex and ShardCount are set only for script tests.
	ShardIndex int `json:"shard-index"`
	ShardCount int `json:"shard-count"`
}

func main() {
	log.SetFlags(0)
	flag.Parse()

	var entries []matrixEntry
	for _, cfg := range _configs {
		for _, gitVersion := range cfg.GitVersions {
			var name strings.Builder
			_, _ = fmt.Fprintf(&name, "os=%s git=%s", cfg.OS, gitVersion)

			// Always run the default tests.
			entries = append(entries, matrixEntry{
				Name:       name.String() + " suite=" + string(defaultTests),
				OS:         cfg.OS,
				GitVersion: gitVersion,
				Suite:      defaultTests,
				Race:       cfg.Race,
				Cover:      cfg.Cover,
			})

			scriptShards := cmp.Or(cfg.ScriptShards, 1)
			name.WriteString(" suite=" + string(scriptTests))
			for shardIndex := range scriptShards {
				name := name.String()
				name += fmt.Sprintf(" shard=[%d/%d]", shardIndex+1, scriptShards)

				entries = append(entries, matrixEntry{
					Name:       name,
					OS:         cfg.OS,
					GitVersion: gitVersion,
					Suite:      scriptTests,
					Race:       cfg.Race,
					Cover:      cfg.Cover,
					ShardIndex: shardIndex,
					ShardCount: scriptShards,
				})
			}
		}
	}

	var output struct {
		Include []matrixEntry `json:"include"`
	}
	output.Include = entries
	enc := json.NewEncoder(os.Stdout)
	if *_indent {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(output); err != nil {
		log.Fatalf("failed to encode JSON: %v", err)
	}
}

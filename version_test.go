package main

import (
	"bytes"
	"runtime/debug"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/testing/stub"
)

func TestVersionFlag(t *testing.T) {
	defer stub.Func(&_generateBuildReport, "commithash timestamp")()

	var (
		exitCode int
		stdout   bytes.Buffer
	)

	_ = versionFlag(true).BeforeReset(&kong.Kong{
		Stdout: &stdout,
		Exit: func(code int) {
			exitCode = code
		},
	})
	assert.Zero(t, exitCode)
	assert.Contains(t, stdout.String(), "git-spice "+_version)
	assert.Contains(t, stdout.String(), "(commithash timestamp)")
}

func TestVersionCmd(t *testing.T) {
	defer stub.Value(&_version, "v1.2.3")()

	t.Run("Default", func(t *testing.T) {
		var stdout bytes.Buffer
		err := new(versionCmd).Run(&kong.Kong{
			Stdout: &stdout,
		})
		require.NoError(t, err)

		assert.Contains(t, stdout.String(), "git-spice v1.2.3")
	})

	t.Run("Short", func(t *testing.T) {
		var stdout bytes.Buffer
		err := (&versionCmd{Short: true}).Run(&kong.Kong{
			Stdout: &stdout,
		})
		require.NoError(t, err)

		assert.Equal(t, "v1.2.3\n", stdout.String())
	})
}

func TestGenerateBuildReport(t *testing.T) {
	tests := []struct {
		name string
		give *debug.BuildInfo
		want string
	}{
		{
			name: "NoBuildInfo",
			give: &debug.BuildInfo{},
		},
		{
			name: "Revision",
			give: &debug.BuildInfo{
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "commithash"},
				},
			},
			want: "commithash",
		},
		{
			name: "RevisionDirty",
			give: &debug.BuildInfo{
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "commithash"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			want: "commithash-dirty",
		},
		{
			name: "TimeOnly",
			give: &debug.BuildInfo{
				Settings: []debug.BuildSetting{
					{Key: "vcs.time", Value: "timestamp"},
				},
			},
			want: "timestamp",
		},
		{
			name: "RevisionAndTime",
			give: &debug.BuildInfo{
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "commithash"},
					{Key: "vcs.time", Value: "timestamp"},
				},
			},
			want: "commithash timestamp",
		},
		{
			name: "RevisionDirtyAndTime",
			give: &debug.BuildInfo{
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "commithash"},
					{Key: "vcs.modified", Value: "true"},
					{Key: "vcs.time", Value: "timestamp"},
				},
			},
			want: "commithash-dirty timestamp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer stub.Func(&_debugReadBuildInfo, tt.give, true)()

			got := _generateBuildReport()
			assert.Equal(t, tt.want, got)
		})
	}
}

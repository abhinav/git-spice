package main

var (
	_linuxConfig = testConfig{
		OS:           "ubuntu-latest",
		GitVersions:  []string{"system", "2.38.0", "2.40.0"},
		ScriptShards: 2,
		Race:         true,
		Cover:        true,
	}

	_windowsConfig = testConfig{
		OS: "windows-latest",
		// We won't compile Git from source on Windows,
		GitVersions: []string{"system"},

		// Windows workers are limited so don't take too many up.
		ScriptShards: 2,

		// Windows tests are quite slow, so we don't enable
		// race detection or coverage tracking for them.
	}

	_configs = []testConfig{
		_linuxConfig,
		_windowsConfig,
	}
)

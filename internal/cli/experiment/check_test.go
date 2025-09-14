package experiment_test

import (
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/cli/experiment"
	"go.abhg.dev/gs/internal/silog"
)

func TestCheck(t *testing.T) {
	var cmd struct {
		experiment.Check

		Cmd      mustNotRunCmd `cmd:"" experiment:"my-experiment"`
		OtherCmd mustNotRunCmd `cmd:""`
	}

	cfg := make(mapEnabler)
	var logBuffer strings.Builder
	app, err := kong.New(
		&cmd,
		kong.Writers(t.Output(), t.Output()),
		kong.Name("my-cli"),
		kong.BindTo(cfg, (*experiment.Enabler)(nil)),
		kong.Bind(silog.New(&logBuffer, nil)),
	)
	require.NoError(t, err)

	t.Run("NotEnabled", func(t *testing.T) {
		delete(cfg, "my-experiment")

		_, err = app.Parse([]string{"cmd"})
		require.Error(t, err)
		assert.Contains(t, logBuffer.String(), "Command is experimental: my-cli cmd")
		assert.Contains(t, logBuffer.String(), "spice.experiment.my-experiment")
	})

	t.Run("Enabled", func(t *testing.T) {
		cfg["my-experiment"] = struct{}{}

		_, err = app.Parse([]string{"cmd"})
		require.NoError(t, err)
	})

	t.Run("NotAnExperiment", func(t *testing.T) {
		_, err = app.Parse([]string{"other-cmd"})
		require.NoError(t, err)
	})
}

type mustNotRunCmd struct{}

func (*mustNotRunCmd) Run() error {
	panic("must not run")
}

type mapEnabler map[string]struct{}

var _ experiment.Enabler = mapEnabler{}

func (e mapEnabler) ExperimentEnabled(name string) bool {
	_, ok := e[name]
	return ok
}

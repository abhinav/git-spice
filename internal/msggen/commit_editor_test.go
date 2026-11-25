package msggen

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestRunCommitEditor_inheritedStreams(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	t.Setenv("MSGGEN_TEST_HELPER", "edit")
	t.Setenv(_fillCommandEnv, exe)
	t.Setenv(_originalEditorEnv, "GIT_EDITOR=original-editor")
	t.Setenv("GIT_EDITOR", "internal-editor")
	input, err := os.CreateTemp(t.TempDir(), "input")
	require.NoError(t, err)
	defer func() { assert.NoError(t, input.Close()) }()
	_, err = input.WriteString("answer\n")
	require.NoError(t, err)
	_, err = input.Seek(0, 0)
	require.NoError(t, err)
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	require.NoError(t, err)
	defer func() { assert.NoError(t, output.Close()) }()
	diagnostics, err := os.CreateTemp(t.TempDir(), "stderr")
	require.NoError(t, err)
	defer func() { assert.NoError(t, diagnostics.Close()) }()
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = input, output, diagnostics
	defer func() { os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr }()
	message, err := os.CreateTemp(t.TempDir(), "message")
	require.NoError(t, err)
	require.NoError(t, message.Close())
	err = RunCommitEditor(t.Context(), &EditRequest{
		File:      message.Name(),
		Operation: "commit-create",
	}, &scriptrun.Runner{Log: silogtest.New(t)})
	require.NoError(t, err)
	for file, want := range map[*os.File]string{
		message:     "answer\n",
		output:      "Progress\n",
		diagnostics: "Question?\n",
	} {
		data, err := os.ReadFile(file.Name())
		require.NoError(t, err)
		assert.Equal(t, want, string(data))
	}
}

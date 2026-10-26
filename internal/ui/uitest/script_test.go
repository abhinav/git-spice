package uitest

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/ui"
)

func TestRunScripts_mixedFileAndDirectoryInputs(t *testing.T) {
	dir := t.TempDir()
	fileScript := filepath.Join(dir, "file.txt")
	scriptDir := filepath.Join(dir, "scripts")

	require.NoError(t, os.WriteFile(fileScript, []byte("record file\n"), 0o644))
	require.NoError(t, os.Mkdir(scriptDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(scriptDir, "dir.txt"), []byte("record dir\n"), 0o644))

	var (
		mu  sync.Mutex
		got []string
	)
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		assert.ElementsMatch(t, []string{"file", "dir"}, got)
	})

	RunScripts(
		t,
		func(testing.TB, *testscript.TestScript, ui.InteractiveView) {},
		&RunScriptsOptions{
			Cmds: map[string]func(*testscript.TestScript, bool, []string){
				"record": func(ts *testscript.TestScript, neg bool, args []string) {
					if neg || len(args) != 1 {
						ts.Fatalf("usage: record value")
					}

					mu.Lock()
					defer mu.Unlock()
					got = append(got, strings.Join(args, " "))
				},
			},
		},
		fileScript,
		scriptDir,
	)
}

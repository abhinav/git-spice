package shamhub

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
	"go.abhg.dev/gs/internal/logtest"
)

type (
	shamHubKey   struct{}
	shamHubValue struct {
		t  testing.TB
		sh *ShamHub
	}
)

// Cmd implements the 'shamhub' command for test scripts.
//
// To install it, run Cmd.Setup in your testscript.Params.Setup,
// and add Cmd.Run as a command in the testscript.Params.Cmds.
type Cmd struct{}

// Setup installs per-script state for the 'shamhub' command
// into the script's environment.
func (c *Cmd) Setup(t testing.TB, e *testscript.Env) {
	e.Values[shamHubKey{}] = &shamHubValue{t: t}
}

// Run implements the 'shamhub' command for test scripts.
// The script MUST have called Cmd.Setup first.
func (c *Cmd) Run(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: shamhub <cmd> [args ...]")
	}

	scriptState := ts.Value(shamHubKey{}).(*shamHubValue)
	sh := scriptState.sh

	cmd, args := args[0], args[1:]
	switch cmd {
	case "init":
		if len(args) != 0 {
			ts.Fatalf("usage: shamhub init")
		}
		if sh != nil {
			ts.Fatalf("ShamHub already initialized")
		}

		t := scriptState.t
		sh, err := New(Config{
			Log: logtest.New(t),
		})
		if err != nil {
			ts.Fatalf("create ShamHub: %s", err)
		}
		ts.Defer(func() {
			if err := sh.Close(); err != nil {
				ts.Logf("close ShamHub: %s", err)
			}
		})
		scriptState.sh = sh

		ts.Logf("Set up ShamHub:\n"+
			"  API URL  = %s\n"+
			"  Git URL  = %s\n"+
			"  Git root = %s",
			sh.APIURL(),
			sh.GitURL(),
			sh.GitRoot(),
		)
		ts.Setenv("SHAMHUB_API_URL", sh.APIURL())
		ts.Setenv("SHAMHUB_URL", sh.GitURL())

	case "new":
		if len(args) != 2 {
			ts.Fatalf("usage: shamhub new <remote> <owner/repo>")
		}
		if sh == nil {
			ts.Fatalf("ShamHub not initialized")
		}

		remote, ownerRepo := args[0], args[1]
		owner, repo, ok := strings.Cut(ownerRepo, "/")
		if !ok {
			ts.Fatalf("invalid owner/repo: %s", ownerRepo)
		}
		repo = strings.TrimSuffix(repo, ".git")
		repoURL, err := sh.NewRepository(owner, repo)
		if err != nil {
			ts.Fatalf("create repository: %s", err)
		}

		ts.Check(ts.Exec("git", "remote", "add", remote, repoURL))

	case "clone":
		if len(args) != 2 {
			ts.Fatalf("usage: shamhub clone <owner/repo> <dir>")
		}
		if sh == nil {
			ts.Fatalf("ShamHub not initialized")
		}

		ownerRepo, dir := args[0], args[1]
		owner, repo, ok := strings.Cut(ownerRepo, "/")
		if !ok {
			ts.Fatalf("invalid owner/repo: %s", ownerRepo)
		}

		err := ts.Exec("git", "clone", sh.RepoURL(owner, repo), dir)
		if err != nil {
			ts.Fatalf("git clone: %s", err)
		}

	case "merge":
		if len(args) != 2 {
			ts.Fatalf("usage: shamhub merge <owner/repo> <pr>")
		}
		if sh == nil {
			ts.Fatalf("ShamHub not initialized")
		}

		ownerRepo, prStr := args[0], args[1]
		owner, repo, ok := strings.Cut(ownerRepo, "/")
		if !ok {
			ts.Fatalf("invalid owner/repo: %s", ownerRepo)
		}
		pr, err := strconv.Atoi(prStr)
		if err != nil {
			ts.Fatalf("invalid PR number: %s", err)
		}

		req := MergeChangeRequest{
			Owner:  owner,
			Repo:   repo,
			Number: pr,
		}
		if at := ts.Getenv("GIT_COMMITTER_DATE"); at != "" {
			t, err := time.Parse(time.RFC3339, at)
			if err != nil {
				ts.Fatalf("invalid time: %s", err)
			}
			req.Time = t
		}
		if name := ts.Getenv("GIT_COMMITTER_NAME"); name != "" {
			req.CommitterName = name
		}
		if email := ts.Getenv("GIT_COMMITTER_EMAIL"); email != "" {
			req.CommitterEmail = email
		}

		ts.Check(sh.MergeChange(req))

	case "register":
		if len(args) != 1 {
			ts.Fatalf("usage: shamhub register <username>")
		}
		if sh == nil {
			ts.Fatalf("ShamHub not initialized")
		}

		username := args[0]
		ts.Check(sh.RegisterUser(username))

	case "dump":
		if len(args) == 0 {
			ts.Fatalf("usage: shamhub dump <cmd> [args ...]")
		}
		if sh == nil {
			ts.Fatalf("ShamHub not initialized")
		}

		var give any

		changes, err := sh.ListChanges()
		if err != nil {
			ts.Fatalf("list changes: %s", err)
		}

		cmd, args := args[0], args[1:]
		switch cmd {
		case "changes":
			if len(args) != 0 {
				ts.Fatalf("usage: shamhub dump changes")
			}
			give = changes

		case "change":
			if len(args) != 1 {
				ts.Fatalf("usage: shamhub dump change <N>")
			}

			want, err := strconv.Atoi(args[0])
			if err != nil {
				ts.Fatalf("invalid change number: %s", err)
			}

			idx := slices.IndexFunc(changes, func(c *Change) bool {
				return c.Number == want
			})
			if idx < 0 {
				ts.Fatalf("CR %d not found", want)
			}
			give = changes[idx]

		default:
			ts.Fatalf("unknown dump command: %s", cmd)
		}

		enc := json.NewEncoder(ts.Stdout())
		enc.SetIndent("", "  ")
		ts.Check(enc.Encode(give))

	default:
		ts.Fatalf("unknown command: %s", cmd)
	}
}

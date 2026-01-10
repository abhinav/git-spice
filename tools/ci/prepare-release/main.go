// prepare-releases prepares a release of git-spice for CI.
//
// # Usage
//
//	go run tools/ci/prepare-release -version=minor
//
// Updates the changelog with unreleased changes,
// and replaces any unreleased feature references
// in source code or documentation with version tags.
//
// Inside a GitHub Actions workflow,
// this will also set the "latest" output for this task.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
)

func main() {
	log := silog.New(os.Stderr, &silog.Options{
		Level: silog.LevelDebug,
	})

	var req prepareRequest
	flag.StringVar(&req.Version, "version", "minor", "Version to prepare")
	flag.StringVar(&req.GithubOutput, "github-output", os.Getenv("GITHUB_OUTPUT"), "GitHub Actions output file (if any)")
	flag.Parse()

	if flag.NArg() > 0 {
		log.Fatalf("prepare-release: unexpected arguments: %v", flag.Args())
	}

	if err := run(log, req); err != nil {
		log.Fatalf("prepare-release: %v", err)
	}
}

type prepareRequest struct {
	Version      string
	GithubOutput string
}

func run(
	log *silog.Logger,
	cmd prepareRequest,
) error {
	if cmd.Version == "" {
		return errors.New("version is required")
	}

	changie := changieClient{
		Log: log.WithPrefix("changie"),
	}
	if err := changie.Batch(cmd.Version); err != nil {
		return fmt.Errorf("batch unreleased changes for %q: %w", cmd.Version, err)
	}

	if err := changie.Merge(); err != nil {
		return fmt.Errorf("merge changelog: %w", err)
	}

	version, err := changie.Latest()
	if err != nil {
		return fmt.Errorf("get latest version: %w", err)
	}

	log.Info("Preparing release", "version", version)

	// If running in a GitHub environment,
	// also set the "latest" output for this task.
	github := githubAction{
		Log:        log.WithPrefix("github"),
		OutputFile: cmd.GithubOutput,
	}
	if err := github.SetOutput("latest", version); err != nil {
		return fmt.Errorf("set action output: %w", err)
	}

	// Replace instances of `<!-- gs:version unreleased -->` in any
	// Markdown file in doc/src with "<!-- gs:version vX.Y.Z -->".
	const docUnreleased = "<!-- gs:version unreleased -->"
	err = sedTree("doc/src",
		strings.NewReplacer(docUnreleased, "<!-- gs:version "+version+" -->"),
		func(path string, ent fs.DirEntry) error {
			if !ent.IsDir() && !strings.HasSuffix(path, ".md") {
				return errSkip
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("replace unreleased tags in doc/src: %w", err)
	}

	// Replace `released:"unreleased"` in any Go file
	// with `released:"vX.Y.Z"`.
	const goUnreleased = `released:"unreleased"`
	err = sedTree(".",
		strings.NewReplacer(goUnreleased, fmt.Sprintf(`released:"%s"`, version)),
		func(path string, d fs.DirEntry) error {
			if d.IsDir() {
				if path == "tools" {
					return errSkip
				}
				return nil
			}

			if !strings.HasSuffix(path, ".go") {
				return errSkip
			}

			return nil
		})
	if err != nil {
		return fmt.Errorf("replace unreleased tags in Go files: %w", err)
	}

	return nil
}

type changieClient struct {
	Log *silog.Logger // required
}

func (c *changieClient) Batch(version string) error {
	cmd := xec.Command(context.Background(), nil, "changie", "batch", version)
	c.Log.Debugf("%v", cmd.Args())
	return wrapExecError(cmd.Run())
}

func (c *changieClient) Merge() error {
	cmd := xec.Command(context.Background(), nil, "changie", "merge")
	c.Log.Debugf("%v", cmd.Args())
	return wrapExecError(cmd.Run())
}

func (c *changieClient) Latest() (string, error) {
	cmd := xec.Command(context.Background(), nil, "changie", "latest")
	c.Log.Debugf("%v", cmd.Args())
	output, err := cmd.Output()
	return strings.TrimSpace(string(output)), wrapExecError(err)
}

func wrapExecError(err error) error {
	var exitErr *xec.ExitError
	if !errors.As(err, &exitErr) {
		return err
	}

	return errors.Join(err, fmt.Errorf("stderr: %s", exitErr.Stderr))
}

type githubAction struct {
	Log        *silog.Logger // required
	OutputFile string
}

func (g *githubAction) SetOutput(name, value string) error {
	if g.OutputFile == "" {
		// Not running in a GitHub Actions environment.
		return nil
	}

	// Equivalent to:
	//
	//	echo "name=value" >> $GITHUB_OUTPUT
	f, err := os.OpenFile(g.OutputFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	g.Log.Debugf("set output %q=%q", name, value)
	_, err = fmt.Fprintf(f, "%s=%s\n", name, value)
	return err
}

// sedVisitor is called on a file or directory
// before walking into its children.
//
// path is relative to the root directory passed to sedTree.
//
// If it returns errSkip, the walk will skip the file or directory.
// If it returns any other error, the walk will stop.
//
// If it returns nil, the file will be transformed with the replacer.
type sedVisitor func(path string, d fs.DirEntry) error

var errSkip = errors.New("skip")

func sedTree(root string, replacer *strings.Replacer, visit sedVisitor) error {
	fsys := os.DirFS(root)
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if err := visit(path, d); err != nil {
			if !errors.Is(err, errSkip) {
				return err
			}

			// Skip was requested.
			// Skip based on whether d is a directory or file.
			if d.IsDir() {
				return fs.SkipDir
			}

			return nil
		}

		if d.IsDir() {
			return nil
		}

		bs, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("read %v: %w", path, err)
		}

		s := string(bs)
		newS := replacer.Replace(s)
		if s == newS {
			return nil
		}

		fpath := filepath.Join(root, path)
		if err := os.WriteFile(fpath, []byte(newS), 0o644); err != nil {
			return fmt.Errorf("write %v: %w", fpath, err)
		}

		return nil
	})
}

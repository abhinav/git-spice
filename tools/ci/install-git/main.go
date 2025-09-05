// install-git downloads and installs a specific version of Git
// to the given prefix.
//
// Dependencies are assumed to already be installed.
package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.abhg.dev/gs/internal/silog"
)

func main() {
	log := silog.New(os.Stderr, &silog.Options{
		Level: silog.LevelDebug,
	})

	var req installRequest
	flag.StringVar(&req.Prefix, "prefix", "", "Destination to install to")
	flag.StringVar(&req.Version, "version", "", "Version to install")
	flag.BoolVar(&req.Debian, "debian", false, "Whether we're on a Debian-based system")
	flag.BoolVar(&req.NoCache, "no-cache", false, "Whether to ignore the cached version")
	flag.Parse()

	if flag.NArg() > 0 {
		log.Fatalf("unexpected arguments: %v", flag.Args())
	}

	if err := run(log, req); err != nil {
		log.Fatalf("install-git: %v", err)
	}
}

type installRequest struct {
	Prefix  string
	Version string

	// Whether to ignore the cached version.
	NoCache bool

	// Whether we're on a Debian-based system.
	// Determines how to install build dependencies.
	Debian bool
}

func (r *installRequest) Validate() (err error) {
	r.Version = strings.TrimPrefix(r.Version, "v")
	if r.Version == "" {
		err = errors.Join(err, errors.New("-version is required"))
	}
	if r.Prefix == "" {
		err = errors.Join(err, errors.New("-prefix is required"))
	}
	return err
}

var _gitBuildDependencies = []string{
	"dh-autoreconf",
	"libcurl4-gnutls-dev",
	"libexpat1-dev",
	"gettext",
	"libz-dev",
	"libssl-dev",
}

func run(log *silog.Logger, req installRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}

	// If prefix is specified and $prefix/bin/git already exists,
	// do nothing.
	binDir := filepath.Join(req.Prefix, "bin")
	gitExe := filepath.Join(binDir, "git")
	if _, err := os.Stat(gitExe); err != nil || req.NoCache {
		// If we're on a Debian-based system, we need to install
		// build dependencies with apt-get.
		if req.Debian {
			installArgs := append([]string{"apt-get", "install"}, _gitBuildDependencies...)
			if err := exec.Command("sudo", installArgs...).Run(); err != nil {
				return fmt.Errorf("apt-get: %w", wrapExecError(err))
			}
		}

		srcDir, cleanup, err := downloadGit(log, req.Version)
		if err != nil {
			return fmt.Errorf("download git: %w", err)
		}
		defer cleanup()
		log.Info("Extracted Git source", "path", srcDir)

		if err := installGit(req.Prefix, srcDir); err != nil {
			return fmt.Errorf("install git: %w", err)
		}

		if info, err := os.Stat(gitExe); err != nil {
			return fmt.Errorf("git not installed: %w", err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("git not executable: %v", gitExe)
		}
	} else {
		log.Info("Requested git version already built",
			"version", req.Version, "path", gitExe)
	}

	return nil
}

func downloadGit(log *silog.Logger, version string) (dir string, cleanup func(), err error) {
	dstPath, err := os.MkdirTemp("", "git-"+version+"-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		// If the operation fails for any reason beyond this point,
		// delete the temporary directory.
		if err != nil {
			err = errors.Join(err, os.RemoveAll(dstPath))
		}
	}()

	dstDir, err := os.OpenRoot(dstPath)
	if err != nil {
		return "", nil, fmt.Errorf("open temp dir: %w", err)
	}
	defer func() { err = errors.Join(err, dstDir.Close()) }()

	gitURL := fmt.Sprintf("https://mirrors.edge.kernel.org/pub/software/scm/git/git-%s.tar.gz", version)
	log.Info("Downloading Git",
		"version", version,
		"url", gitURL)

	res, err := http.Get(gitURL)
	if err != nil {
		return "", nil, fmt.Errorf("http get: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	var resBody io.Reader = res.Body
	if res.ContentLength > 0 {
		progress := &progressWriter{
			N: res.ContentLength,
			W: os.Stderr,
		}
		defer progress.Finish()
		resBody = io.TeeReader(resBody, progress)
	}

	gzipReader, err := gzip.NewReader(resBody)
	if err != nil {
		return "", nil, fmt.Errorf("gunzip: %w", err)
	}

	tarReader := tar.NewReader(gzipReader)
	for {
		hdr, err := tarReader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				// End of archive.
				break
			}

			return "", nil, fmt.Errorf("read tar header: %w", err)
		}

		// Inside the Git archive, the root directory is git-<version>.
		// Strip it from the path.
		_, name, ok := strings.Cut(hdr.Name, string(filepath.Separator))
		if !ok {
			log.Warnf("Skipping unexpected root-level name: %v", hdr.Name)
			continue
		}
		if name == "" {
			// Root git-<version>/ directory. Ignore.
			continue
		}

		if hdr.FileInfo().IsDir() {
			if err := dstDir.Mkdir(name, 0o755); err != nil {
				return "", nil, err
			}
			continue
		}

		err = func() (err error) {
			dst, err := dstDir.Create(name)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, dst.Close()) }()

			if _, err := io.Copy(dst, tarReader); err != nil {
				return fmt.Errorf("copy: %w", err)
			}

			return nil
		}()
		if err != nil {
			return "", nil, fmt.Errorf("unpack %v: %w", name, err)
		}
	}

	return dstPath, func() { _ = os.RemoveAll(dstPath) }, nil
}

func installGit(prefix, srcDir string) error {
	buildCmd := exec.Command("make", "prefix="+prefix)
	buildCmd.Dir = srcDir
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("make: %w", err)
	}

	installCmd := exec.Command("make", "prefix="+prefix, "install")
	installCmd.Dir = srcDir
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("make install: %w", err)
	}

	binDir := filepath.Join(prefix, "bin")
	gitExe := filepath.Join(binDir, "git")
	if info, err := os.Stat(gitExe); err != nil {
		return fmt.Errorf("stat %v: %w", gitExe, err)
	} else if info.Mode()&0o111 == 0 {
		return fmt.Errorf("git not executable: %v", gitExe)
	}

	return nil
}

type progressWriter struct {
	N int64
	W io.Writer

	written    int
	lastUpdate int
}

func (w *progressWriter) Write(bs []byte) (int, error) {
	w.written += len(bs)
	// We want to post updates at 1% increments.
	// If it's been at least w.N/100 bytes since last update, post one.
	if w.written-w.lastUpdate >= int(w.N)/100 {
		fmt.Fprintf(w.W, "\r%v / %v downloaded", w.written, w.N)
		w.lastUpdate = w.written
	}
	return len(bs), nil
}

func (w *progressWriter) Finish() {
	fmt.Fprintln(w.W)
}

func wrapExecError(err error) error {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return err
	}

	return errors.Join(err, fmt.Errorf("stderr: %s", exitErr.Stderr))
}

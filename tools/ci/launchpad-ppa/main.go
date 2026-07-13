// launchpad-ppa builds and optionally uploads Ubuntu PPA source packages.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rogpeppe/go-internal/txtar"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
	"golang.org/x/mod/semver"
)

const (
	_defaultSeries     = "noble"
	_defaultDputTarget = "ppa:abhg/git-spice"
	_packageName       = "git-spice"

	_launchpadGPGKeyIDEnv      = "LAUNCHPAD_GPG_KEY_ID"
	_launchpadGPGPassphraseEnv = "LAUNCHPAD_GPG_PASSPHRASE"
)

//go:embed debian.txtar
var _debianPackagingTxtar []byte

// publishRequest is the normalized command-line request.
type publishRequest struct {
	// Version is the git-spice release version to publish.
	Version string

	// Ref is the Git object exported as upstream source.
	Ref string

	// SourceDateEpoch is the Unix timestamp used for reproducible outputs.
	SourceDateEpoch int64

	// Series lists the Ubuntu series that should receive source uploads.
	Series seriesFlag

	// PPARevision is the Debian package revision suffix for Launchpad retries.
	PPARevision int

	// OmitOrig excludes the upstream orig tarball from every source upload.
	OmitOrig bool

	// Sign controls whether source packages are signed before upload.
	Sign bool

	// Dput controls whether packages are uploaded after signing.
	Dput bool

	// DputTarget is the destination passed to dput.
	DputTarget string
}

// seriesFlag accepts repeated and comma-separated -series values.
type seriesFlag []string

func (f *seriesFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *seriesFlag) Set(value string) error {
	for series := range strings.SplitSeq(value, ",") {
		series = strings.TrimSpace(series)
		if series == "" {
			continue
		}
		*f = append(*f, series)
	}
	return nil
}

func main() {
	log := silog.New(os.Stderr, &silog.Options{
		Level: silog.LevelDebug,
	})

	var req publishRequest
	flag.StringVar(&req.Version, "version", "", "Version to package")
	flag.StringVar(&req.Ref, "ref", "", "Git ref to export")
	flag.Int64Var(&req.SourceDateEpoch, "source-date-epoch", 0,
		"Unix timestamp to use for reproducible package outputs")
	flag.Var(&req.Series, "series", "Ubuntu series to target")
	flag.IntVar(&req.PPARevision, "ppa-revision", 1, "PPA revision number")
	flag.BoolVar(&req.OmitOrig, "omit-orig", false,
		"Omit the upstream orig tarball from every source upload")
	flag.BoolVar(&req.Sign, "sign", false, "Sign packages with Launchpad GPG environment variables")
	flag.BoolVar(&req.Dput, "dput", false, "Upload signed packages with dput")
	flag.StringVar(&req.DputTarget, "dput-target", _defaultDputTarget, "dput upload target")
	flag.Parse()

	if flag.NArg() > 0 {
		log.Fatalf("launchpad-ppa: unexpected arguments: %v", flag.Args())
	}

	if err := run(log, req); err != nil {
		log.Fatalf("launchpad-ppa: %v", err)
	}
}

func run(log *silog.Logger, req publishRequest) error {
	plan, err := newPackagePlan(req)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sourceDateEpoch string
	if !plan.SourceModTime.IsZero() {
		sourceDateEpoch = strconv.FormatInt(plan.SourceModTime.Unix(), 10)
	}
	log.Info("Resolved package request",
		"version", plan.Version,
		"ref", plan.Ref,
		"sourceDateEpoch", sourceDateEpoch,
		"series", strings.Join(plan.Series, ","),
		"ppaRevision", plan.PPARevision,
		"omitOrig", plan.OmitOrig,
		"dput", plan.Dput,
		"dputTarget", plan.DputTarget)

	root, err := xec.Command(ctx, log, "git", "rev-parse", "--show-toplevel").
		OutputChomp()
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	log.Info("Resolved repository root", "path", root)

	workDir, err := os.MkdirTemp("", "git-spice-ppa.*")
	if err != nil {
		return fmt.Errorf("create temporary directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(workDir); err != nil {
			log.Warn("Remove temporary directory", "path", workDir, "error", err)
		}
	}()
	log.Info("Created temporary workspace", "path", workDir)

	sourceDir := filepath.Join(workDir, _packageName+"-"+plan.UpstreamVersion)
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		return fmt.Errorf("create source directory: %w", err)
	}

	if err := exportSource(ctx, log, root, plan.Ref, sourceDir); err != nil {
		return fmt.Errorf("export source: %w", err)
	}
	if err := writeDebianPackaging(log, sourceDir); err != nil {
		return fmt.Errorf("write Debian packaging: %w", err)
	}

	log.Info("Vendoring Go modules")
	if err := xec.Command(ctx, log, "go", "mod", "vendor").
		WithDir(sourceDir).
		Run(); err != nil {
		return fmt.Errorf("vendor Go modules: %w", err)
	}

	origTar := filepath.Join(
		workDir,
		fmt.Sprintf("%s_%s.orig.tar.gz", _packageName, plan.UpstreamVersion),
	)
	mtime := plan.SourceModTime
	if mtime.IsZero() {
		var err error
		mtime, err = sourceModTime(ctx, log, root, plan.Ref)
		if err != nil {
			return fmt.Errorf("resolve source modification time: %w", err)
		}
	}
	if err := writeOrigTar(log, sourceDir, origTar, mtime); err != nil {
		return fmt.Errorf("write orig tarball: %w", err)
	}

	if plan.Sign != nil {
		if err := plan.Sign.WritePassphraseFile(workDir); err != nil {
			return fmt.Errorf("write signing passphrase: %w", err)
		}
	}

	var dputCommands []string
	for i, series := range plan.Series {
		changes, err := buildSeries(
			ctx, log,
			root, sourceDir, workDir, origTar,
			plan, series, plan.sourceUploadMode(i), mtime)
		if err != nil {
			return fmt.Errorf("build %s: %w", series, err)
		}

		if plan.Sign != nil {
			if err := signChanges(ctx, log, changes, *plan.Sign); err != nil {
				return fmt.Errorf("sign %s: %w", series, err)
			}
		}

		if plan.Dput {
			log.Info("Uploading source package", "series", series, "changes", changes)
			output, flushOutput := silog.Writer(log.WithPrefix("dput"), silog.LevelInfo)
			err := xec.Command(ctx, log, "dput", "--unchecked", plan.DputTarget, changes).
				WithStdout(output).
				WithStderr(output).
				Run()
			flushOutput()
			if err != nil {
				return fmt.Errorf("dput %s: %w", series, err)
			}
		} else {
			dputCommands = append(dputCommands, renderDputCommand(plan.DputTarget, changes))
		}
	}

	if !plan.Dput {
		log.Info("Dry run complete; dput was not requested")
		for _, command := range dputCommands {
			log.Info("WOULD run " + command)
		}
	}

	return nil
}

func writeDebianPackaging(log *silog.Logger, sourceDir string) error {
	log.Info("Writing embedded Debian packaging")

	if err := os.RemoveAll(filepath.Join(sourceDir, "debian")); err != nil {
		return fmt.Errorf("remove debian directory: %w", err)
	}
	if err := txtar.Write(txtar.Parse(_debianPackagingTxtar), sourceDir); err != nil {
		return fmt.Errorf("write txtar: %w", err)
	}
	if err := os.Chmod(filepath.Join(sourceDir, "debian", "rules"), 0o755); err != nil {
		return fmt.Errorf("make debian/rules executable: %w", err)
	}
	return nil
}

// signConfig identifies the Launchpad signing material for debsign.
type signConfig struct {
	// KeyID is the GPG key ID passed to debsign.
	KeyID string

	// Passphrase is written to PassphraseFile before signing.
	Passphrase string

	// PassphraseFile is read by gpg when debsign invokes it.
	PassphraseFile string
}

func signConfigFromEnv(enabled bool) (*signConfig, error) {
	if !enabled {
		return nil, nil
	}

	keyID := os.Getenv(_launchpadGPGKeyIDEnv)
	if keyID == "" {
		return nil, fmt.Errorf("%s is required with -sign", _launchpadGPGKeyIDEnv)
	}

	passphrase := os.Getenv(_launchpadGPGPassphraseEnv)
	if passphrase == "" {
		return nil, fmt.Errorf("%s is required with -sign", _launchpadGPGPassphraseEnv)
	}

	return &signConfig{
		KeyID:      keyID,
		Passphrase: passphrase,
	}, nil
}

func (c *signConfig) WritePassphraseFile(dir string) error {
	c.PassphraseFile = filepath.Join(dir, "launchpad-gpg-passphrase")
	return os.WriteFile(c.PassphraseFile, []byte(c.Passphrase), 0o600)
}

// packagePlan is the derived packaging plan shared by all series builds.
type packagePlan struct {
	// Version is the user-facing release version, including its leading "v".
	Version string

	// UpstreamVersion is the Debian upstream version without a leading "v".
	UpstreamVersion string

	// BaseDebianVersion is the Debian version before the Ubuntu series suffix.
	BaseDebianVersion string

	// SourceModTime is used for reproducible archive and package mtimes.
	SourceModTime time.Time

	// PPARevision is the Launchpad PPA source package revision.
	PPARevision int

	// OmitOrig excludes the upstream orig tarball from every source upload.
	OmitOrig bool

	// Ref is the Git object exported as upstream source.
	Ref string

	// Series lists the Ubuntu series to publish.
	Series []string

	// Sign is nil when source packages should remain unsigned.
	Sign *signConfig

	// Dput controls whether packages are uploaded after signing.
	Dput bool

	// DputTarget is the destination passed to dput.
	DputTarget string
}

func (p packagePlan) sourceUploadMode(seriesIndex int) sourceUploadMode {
	if p.OmitOrig || seriesIndex > 0 {
		return sourceUploadWithoutOrig
	}
	return sourceUploadWithOrig
}

// sourceUploadMode controls whether the generated .changes upload manifest
// includes the upstream orig tarball.
type sourceUploadMode int

const (
	sourceUploadWithOrig sourceUploadMode = iota
	sourceUploadWithoutOrig
)

func (m sourceUploadMode) String() string {
	switch m {
	case sourceUploadWithOrig:
		return "with-orig"
	case sourceUploadWithoutOrig:
		return "without-orig"
	default:
		return fmt.Sprintf("sourceUploadMode(%d)", m)
	}
}

func (m sourceUploadMode) dpkgBuildpackageArgs() []string {
	args := []string{
		"-S", // build source package only
	}
	if m == sourceUploadWithoutOrig {
		args = append(args,
			"-sd", // omit the upstream orig tarball from .changes
		)
	}
	return append(args,
		"-us", // leave the .dsc unsigned
		"-uc", // leave .buildinfo and .changes unsigned
		"-d",  // skip build dependency checks on CI runners
	)
}

func newPackagePlan(req publishRequest) (packagePlan, error) {
	var err error

	if req.Version == "" {
		err = errors.Join(err, errors.New("-version is required"))
	}

	if req.Version != "" && !semver.IsValid(req.Version) {
		err = errors.Join(err, fmt.Errorf("version must be a valid semantic version: %q", req.Version))
	}
	upstreamVersion := strings.TrimPrefix(req.Version, "v")

	if req.Ref == "" {
		req.Ref = req.Version
	}

	var sourceModTime time.Time
	if req.SourceDateEpoch < 0 {
		err = errors.Join(err,
			fmt.Errorf("-source-date-epoch must be positive: %d",
				req.SourceDateEpoch))
	} else if req.SourceDateEpoch > 0 {
		sourceModTime = time.Unix(req.SourceDateEpoch, 0).UTC()
	}

	series := slices.Clone(req.Series)
	if len(series) == 0 {
		series = []string{_defaultSeries}
	}

	if req.PPARevision <= 0 {
		err = errors.Join(err, fmt.Errorf("PPA revision must be positive: %d", req.PPARevision))
	}

	sign, signErr := signConfigFromEnv(req.Sign)
	err = errors.Join(err, signErr)

	if req.DputTarget == "" {
		err = errors.Join(err, errors.New("-dput-target is required"))
	}

	return packagePlan{
		Version:         req.Version,
		UpstreamVersion: upstreamVersion,
		BaseDebianVersion: fmt.Sprintf(
			"%s-1~ppa%d",
			upstreamVersion,
			req.PPARevision,
		),
		SourceModTime: sourceModTime,
		PPARevision:   req.PPARevision,
		OmitOrig:      req.OmitOrig,
		Ref:           req.Ref,
		Series:        series,
		Sign:          sign,
		Dput:          req.Dput,
		DputTarget:    req.DputTarget,
	}, err
}

func exportSource(
	ctx context.Context,
	log *silog.Logger,
	root string,
	ref string,
	sourceDir string,
) error {
	log.Info("Exporting source", "ref", ref, "path", sourceDir)

	// The source package does not need LFS media content,
	// and CI runners may not have git-lfs installed.
	git := xec.Command(ctx, log, "git",
		"-C", root,
		"-c", "filter.lfs.required=false",
		"-c", "filter.lfs.process=",
		"-c", "filter.lfs.smudge=cat",
		"archive",
		"--format=tar",
		"--worktree-attributes",
		ref,
	)
	out, err := git.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe git archive: %w", err)
	}

	tarCmd := xec.Command(ctx, log, "tar", "-C", sourceDir, "-xf", "-").
		WithStdin(out)

	if err := git.Start(); err != nil {
		return fmt.Errorf("start git archive: %w", err)
	}
	if err := tarCmd.Run(); err != nil {
		_ = git.Kill()
		return fmt.Errorf("extract source: %w", err)
	}
	if err := git.Wait(); err != nil {
		return fmt.Errorf("wait for git archive: %w", err)
	}

	return nil
}

func sourceModTime(
	ctx context.Context,
	log *silog.Logger,
	root string,
	ref string,
) (time.Time, error) {
	out, err := xec.Command(
		ctx,
		log,
		"git",
		"log",
		"-1",
		"--format=%ct",
		ref,
	).
		WithDir(root).
		OutputChomp()
	if err != nil {
		return time.Time{}, fmt.Errorf("read commit timestamp: %w", err)
	}

	sec, err := strconv.ParseInt(out, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse commit timestamp %q: %w", out, err)
	}

	return time.Unix(sec, 0).UTC(), nil
}

func writeOrigTar(
	log *silog.Logger,
	sourceDir string,
	dest string,
	mtime time.Time,
) error {
	log.Info("Writing deterministic orig tarball", "path", dest)

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("create gzip writer: %w", err)
	}
	gz.Name = ""
	gz.ModTime = mtime

	tw := tar.NewWriter(gz)
	if err := writeTarTree(tw, sourceDir, mtime); err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return fmt.Errorf("close tar writer: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("close gzip writer: %w", err)
	}

	return nil
}

func writeTarTree(tw *tar.Writer, sourceDir string, mtime time.Time) error {
	return filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("relativize %s: %w", path, err)
		}
		if rel == "." {
			return nil
		}

		name := "./" + filepath.ToSlash(rel)
		if name == "./debian" || strings.HasPrefix(name, "./debian/") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}

		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return fmt.Errorf("read link %s: %w", path, err)
			}
		}

		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return fmt.Errorf("create tar header for %s: %w", path, err)
		}
		header.Name = name
		header.ModTime = mtime
		header.AccessTime = mtime
		header.ChangeTime = mtime
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		header.Format = tar.FormatPAX

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("write tar header for %s: %w", path, err)
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}

		if _, err := io.Copy(tw, f); err != nil {
			_ = f.Close()
			return fmt.Errorf("write %s: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close %s: %w", path, err)
		}

		return nil
	})
}

func buildSeries(
	ctx context.Context,
	log *silog.Logger,
	root string,
	sourceDir string,
	workDir string,
	origTar string,
	plan packagePlan,
	series string,
	uploadMode sourceUploadMode,
	mtime time.Time,
) (string, error) {
	log.Info("Building source package", "series", series, "uploadMode", uploadMode)

	ubuntuVersion, err := ubuntuVersionForSeries(ctx, log, series)
	if err != nil {
		return "", fmt.Errorf("resolve Ubuntu version: %w", err)
	}
	debianVersion := plan.BaseDebianVersion + "~ubuntu" + ubuntuVersion + ".1"

	if err := writeDebianChangelog(
		sourceDir, plan, debianVersion, series, mtime); err != nil {
		return "", err
	}

	if err := cleanupBuildProducts(workDir, debianVersion); err != nil {
		return "", fmt.Errorf("clean previous build products: %w", err)
	}

	if err := xec.Command(ctx, log, "dpkg-buildpackage", uploadMode.dpkgBuildpackageArgs()...).
		WithDir(sourceDir).
		AppendEnv("SOURCE_DATE_EPOCH=" + strconv.FormatInt(mtime.Unix(), 10)).
		Run(); err != nil {
		return "", fmt.Errorf("dpkg-buildpackage: %w", err)
	}

	outDir := filepath.Join(root, "dist", "debian", series)
	if err := os.RemoveAll(outDir); err != nil {
		return "", fmt.Errorf("remove output directory: %w", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}
	if uploadMode == sourceUploadWithOrig {
		// The first series upload must carry the upstream orig tarball.
		// Later source uploads can refer to the tarball Launchpad already has.
		if err := copyFile(origTar, filepath.Join(outDir, filepath.Base(origTar))); err != nil {
			return "", fmt.Errorf("copy orig tarball: %w", err)
		}
	} else {
		// With -sd, dpkg-buildpackage omits the orig tarball from .changes.
		// Do not copy it next to the upload manifest,
		// so the dry-run output matches what dput will upload.
		log.Info("Omitting orig tarball from source upload",
			"series", series,
			"origTar", filepath.Base(origTar))
	}

	pattern := filepath.Join(workDir, _packageName+"_"+debianVersion+"*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("glob build products: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no build products matched %s", pattern)
	}

	var changes string
	for _, path := range matches {
		dest := filepath.Join(outDir, filepath.Base(path))
		if err := copyFile(path, dest); err != nil {
			return "", fmt.Errorf("copy %s: %w", path, err)
		}
		if strings.HasSuffix(dest, "_source.changes") {
			changes = dest
		}
	}
	if changes == "" {
		return "", errors.New("source changes file not produced")
	}

	log.Info("Wrote source package", "series", series, "changes", changes)
	return changes, nil
}

func ubuntuVersionForSeries(
	ctx context.Context,
	log *silog.Logger,
	series string,
) (string, error) {
	out, err := xec.Command(ctx, log,
		"ubuntu-distro-info",
		"--series="+series,
		"--release",
	).OutputChomp()
	if err != nil {
		return "", fmt.Errorf("ubuntu-distro-info: %w", err)
	}

	out = strings.TrimSpace(out)
	if out == "" {
		return "", errors.New("empty Ubuntu version")
	}

	out, _, _ = strings.Cut(out, " ")
	return out, nil
}

func writeDebianChangelog(
	sourceDir string,
	plan packagePlan,
	debianVersion string,
	series string,
	mtime time.Time,
) error {
	changelog := fmt.Sprintf(`%s (%s) %s; urgency=medium

  * Release git-spice %s.

 -- Abhinav Gupta <mail@abhinavg.net>  %s
`, _packageName, debianVersion, series, plan.UpstreamVersion, mtime.Format(time.RFC1123Z))

	if err := os.WriteFile(filepath.Join(sourceDir, "debian", "changelog"), []byte(changelog), 0o644); err != nil {
		return fmt.Errorf("write debian/changelog: %w", err)
	}
	return nil
}

func cleanupBuildProducts(workDir string, debianVersion string) error {
	matches, err := filepath.Glob(filepath.Join(workDir, _packageName+"_"+debianVersion+"*"))
	if err != nil {
		return err
	}
	for _, path := range matches {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func signChanges(
	ctx context.Context,
	log *silog.Logger,
	changes string,
	sign signConfig,
) error {
	log.Info("Signing source package", "changes", changes, "key", sign.KeyID)

	gpgCommand := "gpg --batch --pinentry-mode loopback --passphrase-file " + sign.PassphraseFile
	if err := xec.Command(ctx, log,
		"debsign",
		"-k"+sign.KeyID,
		"-p"+gpgCommand,
		changes,
	).Run(); err != nil {
		return fmt.Errorf("debsign: %w", err)
	}

	return nil
}

func copyFile(src string, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	info, err := srcFile.Stat()
	if err != nil {
		return err
	}

	destFile, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() { _ = destFile.Close() }()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return err
	}

	return destFile.Close()
}

func renderDputCommand(target string, changes string) string {
	var buf bytes.Buffer
	buf.WriteString("dput ")
	buf.WriteString(target)
	buf.WriteByte(' ')
	buf.WriteString(changes)
	return buf.String()
}

package shamhub

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/secret/secretserver"
	"go.abhg.dev/gs/internal/silog"
)

// ServeCLI runs the shamhub-serve command line program.
//
// ServeCLI reads arguments from os.Args,
// writes connection details to os.Stdout or the configured -env-file,
// writes errors to os.Stderr,
// and blocks until the process receives an interrupt or SIGTERM.
func ServeCLI() (exitCode int) {
	if err := runServeCLI(
		context.Background(),
		os.Args[1:],
		os.Stdout,
		os.Stderr,
	); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runServeCLI(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	flags := flag.NewFlagSet("shamhub-serve", flag.ContinueOnError)
	flags.SetOutput(stderr)

	apiAddr := flags.String(
		"api-addr",
		"",
		"API listen address (default: loopback with any port)",
	)
	gitAddr := flags.String(
		"git-addr",
		"",
		"Git HTTP listen address (default: loopback with any port)",
	)
	gitRoot := flags.String(
		"git-root",
		"",
		"Git repository storage root (default: temporary directory)",
	)
	keepGitRoot := flags.Bool(
		"keep-git-root",
		false,
		"keep the Git root on shutdown",
	)
	adminToken := flags.String(
		"admin-token",
		"",
		"admin token (default: generated random token)",
	)
	envFile := flags.String("env-file", "", "write shell exports to a file")
	jsonOutput := flags.Bool("json", false, "print JSON instead of shell exports")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: shamhub-serve [flags]")
	}

	sh, err := New(Config{
		APIAddr:     *apiAddr,
		GitAddr:     *gitAddr,
		GitRoot:     *gitRoot,
		KeepGitRoot: *keepGitRoot,
		AdminToken:  *adminToken,
		Log:         silog.New(stderr, nil),
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = sh.Close()
	}()

	secretServer, err := secretserver.NewServer(new(secret.MemoryStash))
	if err != nil {
		return fmt.Errorf("create secret server: %w", err)
	}
	defer func() {
		_ = secretServer.Close()
	}()

	env := serverEnv{
		APIURL:     sh.APIURL(),
		GitURL:     sh.GitURL(),
		AdminToken: sh.AdminToken(),
		GitRoot:    sh.GitRoot(),
		SecretURL:  secretServer.URL(),
	}
	if *envFile != "" {
		if err := os.WriteFile(*envFile, []byte(env.shell()), 0o644); err != nil {
			return fmt.Errorf("write env file: %w", err)
		}
	}
	if *jsonOutput {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(env); err != nil {
			return fmt.Errorf("write JSON: %w", err)
		}
	} else {
		fmt.Fprint(stdout, env.shell())
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}

type serverEnv struct {
	APIURL     string `json:"apiUrl"`
	GitURL     string `json:"gitUrl"`
	AdminToken string `json:"adminToken"`
	GitRoot    string `json:"gitRoot"`
	SecretURL  string `json:"secretUrl"`
}

func (e *serverEnv) shell() string {
	return fmt.Sprintf(
		"export SHAMHUB_API_URL=%q\n"+
			"export SHAMHUB_URL=%q\n"+
			"export SHAMHUB_ADMIN_TOKEN=%q\n"+
			"export SHAMHUB_GIT_ROOT=%q\n"+
			"export SHAMHUB_SECRET_URL=%q\n",
		e.APIURL,
		e.GitURL,
		e.AdminToken,
		e.GitRoot,
		e.SecretURL,
	)
}

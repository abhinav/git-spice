package shamhub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/xec"
	"gopkg.in/yaml.v3"
)

// CLI runs the shamhub command line program using process-global IO.
//
// CLI reads arguments from os.Args,
// writes command output to os.Stdout,
// writes errors and subprocess stderr to os.Stderr,
// and reads ShamHub connection details from the environment.
// Commands require SHAMHUB_API_URL, SHAMHUB_URL, and SHAMHUB_ADMIN_TOKEN.
func CLI() (exitCode int) {
	if err := runCLI(
		context.Background(),
		os.Args[1:],
		os.Getenv,
		os.Stdout,
		os.Stderr,
	); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runCLI(
	ctx context.Context,
	args []string,
	getenv func(string) string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if len(args) == 0 {
		return errors.New("usage: shamhub <cmd> [args ...]")
	}

	client, err := newShamHubCLIAdminClient(getenv)
	if err != nil {
		return err
	}

	cli := shamhubCLI{
		ctx:    ctx,
		client: client,
		getenv: getenv,
		stdout: stdout,
		stderr: stderr,
	}
	return cli.run(args)
}

// shamhubCLI owns command dispatch and process-facing streams for one run.
type shamhubCLI struct {
	ctx    context.Context
	client *shamhubCLIAdminClient
	getenv func(string) string
	stdout io.Writer
	stderr io.Writer
}

func (c *shamhubCLI) run(args []string) error {
	cmd, args := args[0], args[1:]
	switch cmd {
	case "comment":
		return c.comment(args)
	case "new":
		return c.newRepository(args)
	case "clone":
		return c.cloneRepository(args)
	case "fork":
		return c.forkRepository(args)
	case "config":
		return c.config(args)
	case "set-status":
		return c.setStatus(args)
	case "set-mergeability":
		return c.setMergeability(args)
	case "merge":
		return c.merge(args)
	case "reject":
		return c.reject(args)
	case "delete-comment":
		return c.deleteComment(args)
	case "register":
		return c.register(args)
	case "dump":
		return c.dump(args)
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

// shamhubCLIAdminClient is the CLI's REST transport for ShamHub admin routes.
type shamhubCLIAdminClient struct {
	apiURL string
	gitURL string
	token  string
	client *http.Client
}

func newShamHubCLIAdminClient(
	getenv func(string) string,
) (*shamhubCLIAdminClient, error) {
	apiURL := getenv("SHAMHUB_API_URL")
	if apiURL == "" {
		return nil, errors.New("SHAMHUB_API_URL is required")
	}
	if _, err := url.Parse(apiURL); err != nil {
		return nil, fmt.Errorf("parse SHAMHUB_API_URL: %w", err)
	}

	gitURL := getenv("SHAMHUB_URL")
	if gitURL == "" {
		return nil, errors.New("SHAMHUB_URL is required")
	}
	if _, err := url.Parse(gitURL); err != nil {
		return nil, fmt.Errorf("parse SHAMHUB_URL: %w", err)
	}

	token := getenv("SHAMHUB_ADMIN_TOKEN")
	if token == "" {
		return nil, errors.New("SHAMHUB_ADMIN_TOKEN is required")
	}

	return &shamhubCLIAdminClient{
		apiURL: strings.TrimRight(apiURL, "/"),
		gitURL: strings.TrimRight(gitURL, "/"),
		token:  token,
		client: http.DefaultClient,
	}, nil
}

// Get sends an authenticated GET request to a ShamHub admin endpoint.
func (c *shamhubCLIAdminClient) Get(
	ctx context.Context,
	path string,
	res any,
) error {
	return c.do(ctx, http.MethodGet, path, nil, res)
}

// Post sends an authenticated POST request to a ShamHub admin endpoint.
func (c *shamhubCLIAdminClient) Post(
	ctx context.Context,
	path string,
	req any,
	res any,
) error {
	return c.do(ctx, http.MethodPost, path, req, res)
}

// Patch sends an authenticated PATCH request to a ShamHub admin endpoint.
func (c *shamhubCLIAdminClient) Patch(
	ctx context.Context,
	path string,
	req any,
	res any,
) error {
	return c.do(ctx, http.MethodPatch, path, req, res)
}

// Delete sends an authenticated DELETE request to a ShamHub admin endpoint.
func (c *shamhubCLIAdminClient) Delete(
	ctx context.Context,
	path string,
	res any,
) error {
	return c.do(ctx, http.MethodDelete, path, nil, res)
}

func (c *shamhubCLIAdminClient) do(
	ctx context.Context,
	method string,
	path string,
	req any,
	res any,
) error {
	var body io.Reader
	if req != nil {
		bs, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(bs)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		method,
		c.apiURL+path,
		body,
	)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("ShamHub-Admin-Token", c.token)

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()

	resBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", httpResp.StatusCode, resBody)
	}
	if res == nil {
		return nil
	}
	if err := json.Unmarshal(resBody, res); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// Comment commands seed and mutate review comments for test scenarios.
func (c *shamhubCLI) comment(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: shamhub comment <post|edit|delete> [args ...]")
	}

	switch args[0] {
	case "post":
		return c.postComment(args[1:])
	case "edit":
		return c.editComment(args[1:])
	case "delete":
		return c.deleteComment(args[1:])
	default:
		return fmt.Errorf("unknown shamhub comment command: %s", args[0])
	}
}

func (c *shamhubCLI) postComment(args []string) error {
	flags := flag.NewFlagSet("shamhub comment post", flag.ContinueOnError)
	flags.SetOutput(c.stderr)
	id := flags.Int("id", 0, "explicit comment ID")
	resolvable := flags.Bool("resolvable", false, "mark comment as resolvable")
	resolved := flags.Bool("resolved", false, "mark comment as resolved")
	if err := flags.Parse(args); err != nil {
		return err
	}
	args = flags.Args()
	if len(args) != 3 {
		return errors.New(
			"usage: shamhub comment post [-id=N] [-resolvable] " +
				"[-resolved] <owner/repo> <change> <body>",
		)
	}

	owner, repo, err := parseOwnerRepo(args[0])
	if err != nil {
		return err
	}
	change, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("invalid change number %q: %w", args[1], err)
	}

	var res adminPostCommentResponse
	err = c.client.Post(c.ctx, "/_shamhub/admin/comments", adminPostCommentBody{
		Owner:      owner,
		Repo:       repo,
		Change:     change,
		ID:         *id,
		Body:       args[2],
		Resolvable: *resolvable,
		Resolved:   *resolved,
	}, &res)
	if err != nil {
		return err
	}
	fmt.Fprintln(c.stdout, res.ID)
	return nil
}

func (c *shamhubCLI) editComment(args []string) error {
	flags := flag.NewFlagSet("shamhub comment edit", flag.ContinueOnError)
	flags.SetOutput(c.stderr)
	var resolved nullableBoolFlag
	flags.Var(&resolved, "resolved", "set whether the comment is resolved")
	if err := flags.Parse(args); err != nil {
		return err
	}
	args = flags.Args()
	if len(args) != 1 {
		return errors.New("usage: shamhub comment edit [-resolved=true|false] <id>")
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid comment ID %q: %w", args[0], err)
	}

	return c.client.Patch(
		c.ctx,
		"/_shamhub/admin/comments/"+strconv.Itoa(id),
		adminEditCommentBody{Resolved: resolved.Ptr()},
		&adminEditCommentResponse{},
	)
}

func (c *shamhubCLI) deleteComment(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: shamhub comment delete <id>")
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid comment ID %q: %w", args[0], err)
	}
	return c.client.Delete(
		c.ctx,
		"/_shamhub/admin/comments/"+strconv.Itoa(id),
		&adminDeleteCommentResponse{},
	)
}

// Repository commands create, clone, and fork bare Git repositories.
func (c *shamhubCLI) newRepository(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: shamhub new <remote> <owner/repo>")
	}

	owner, repo, err := parseOwnerRepo(args[1])
	if err != nil {
		return err
	}

	var res adminRepositoryResponse
	if err := c.client.Post(c.ctx, "/_shamhub/admin/repos", adminNewRepositoryBody{
		Owner: owner,
		Repo:  repo,
	}, &res); err != nil {
		return err
	}
	return c.runGit("remote", "add", args[0], res.URL)
}

func (c *shamhubCLI) cloneRepository(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: shamhub clone <owner/repo> <dir>")
	}

	owner, repo, err := parseOwnerRepo(args[0])
	if err != nil {
		return err
	}
	return c.runGit(
		"clone",
		c.client.gitURL+"/"+owner+"/"+repo+".git",
		args[1],
	)
}

func (c *shamhubCLI) forkRepository(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: shamhub fork <owner/repo> <fork-owner>")
	}

	owner, repo, err := parseOwnerRepo(args[0])
	if err != nil {
		return err
	}

	var res adminRepositoryResponse
	if err := c.client.Post(c.ctx, "/_shamhub/admin/repos/fork", adminForkRepositoryBody{
		Owner:     owner,
		Repo:      repo,
		ForkOwner: args[1],
	}, &res); err != nil {
		return err
	}
	fmt.Fprintf(c.stderr, "Forked %s/%s to %s\n", owner, repo, res.URL)
	return nil
}

// Configuration commands adjust ShamHub server behavior for tests.
func (c *shamhubCLI) config(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: shamhub config <key> <value>")
	}

	return c.client.Post(c.ctx, "/_shamhub/admin/config", adminConfigBody{
		Key:   args[0],
		Value: args[1],
	}, &adminConfigResponse{})
}

// Change-control commands mutate forge state that git-spice reads back.
func (c *shamhubCLI) setStatus(args []string) error {
	flags := flag.NewFlagSet("shamhub set-status", flag.ContinueOnError)
	flags.SetOutput(c.stderr)
	name := flags.String("name", "", "status check name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	args = flags.Args()
	if len(args) != 3 || *name == "" {
		return errors.New(
			"shamhub set-status --name <name> <owner/repo> <pr> <status>",
		)
	}

	owner, repo, err := parseOwnerRepo(args[0])
	if err != nil {
		return err
	}
	pr, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("invalid PR number %q: %w", args[1], err)
	}

	return c.client.Post(
		c.ctx,
		adminChangePath(owner, repo, pr, "checks"),
		adminSetStatusBody{Name: *name, State: args[2]},
		&adminSetStatusResponse{},
	)
}

func (c *shamhubCLI) setMergeability(args []string) error {
	flags := flag.NewFlagSet("shamhub set-mergeability", flag.ContinueOnError)
	flags.SetOutput(c.stderr)
	reason := flags.String("reason", "", "mergeability reason")
	if err := flags.Parse(args); err != nil {
		return err
	}
	args = flags.Args()
	if len(args) != 3 {
		return errors.New(
			"shamhub set-mergeability [-reason <reason>] " +
				"<owner/repo> <pr> <state>",
		)
	}

	owner, repo, err := parseOwnerRepo(args[0])
	if err != nil {
		return err
	}
	pr, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("invalid PR number %q: %w", args[1], err)
	}

	return c.client.Post(
		c.ctx,
		adminChangePath(owner, repo, pr, "mergeability"),
		adminSetMergeabilityBody{State: args[2], Reason: *reason},
		&adminSetMergeabilityResponse{},
	)
}

func (c *shamhubCLI) merge(args []string) error {
	flags := flag.NewFlagSet("shamhub merge", flag.ContinueOnError)
	flags.SetOutput(c.stderr)
	prune := flags.Bool("prune", false, "prune the branch after merging")
	squash := flags.Bool("squash", false, "squash-merge the commit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	args = flags.Args()
	if len(args) != 2 {
		return errors.New("usage: shamhub merge [-prune] [-squash] <owner/repo> <pr>")
	}

	owner, repo, err := parseOwnerRepo(args[0])
	if err != nil {
		return err
	}
	pr, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("invalid PR number %q: %w", args[1], err)
	}

	req := adminMergeChangeBody{
		DeleteBranch: *prune,
		Squash:       *squash,
	}
	if at := c.getenv("GIT_COMMITTER_DATE"); at != "" {
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return fmt.Errorf("invalid GIT_COMMITTER_DATE: %w", err)
		}
		req.Time = t
	}
	req.CommitterName = c.getenv("GIT_COMMITTER_NAME")
	req.CommitterEmail = c.getenv("GIT_COMMITTER_EMAIL")

	return c.client.Post(
		c.ctx,
		adminChangePath(owner, repo, pr, "merge"),
		req,
		&adminMergeChangeResponse{},
	)
}

func (c *shamhubCLI) reject(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: shamhub reject <owner/repo> <pr>")
	}

	owner, repo, err := parseOwnerRepo(args[0])
	if err != nil {
		return err
	}
	pr, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("invalid PR number %q: %w", args[1], err)
	}

	return c.client.Post(
		c.ctx,
		adminChangePath(owner, repo, pr, "reject"),
		adminRejectChangeBody{},
		&adminRejectChangeResponse{},
	)
}

func (c *shamhubCLI) register(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: shamhub register <username>")
	}

	return c.client.Post(c.ctx, "/_shamhub/admin/users", adminRegisterUserBody{
		Username: args[0],
	}, &adminRegisterUserResponse{})
}

// Dump commands render ShamHub state in script-friendly formats.
func (c *shamhubCLI) dump(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: shamhub dump <cmd> [args ...]")
	}

	switch args[0] {
	case "changes":
		if len(args) != 1 {
			return errors.New("usage: shamhub dump changes")
		}

		var res adminDumpChangesResponse
		if err := c.client.Get(c.ctx, "/_shamhub/admin/dump/changes", &res); err != nil {
			return err
		}
		return encodeJSON(c.stdout, res.Changes)

	case "comments":
		u := "/_shamhub/admin/dump/comments"
		if len(args) > 1 {
			q := make(url.Values)
			for _, change := range args[1:] {
				if _, err := strconv.Atoi(change); err != nil {
					return fmt.Errorf("invalid change number %q: %w", change, err)
				}
				q.Add("change", change)
			}
			u += "?" + q.Encode()
		}

		var res adminDumpCommentsResponse
		if err := c.client.Get(c.ctx, u, &res); err != nil {
			return err
		}
		type changeComment struct {
			Change int    `yaml:"change"`
			Body   string `yaml:"body"`
		}
		comments := make([]changeComment, 0, len(res.Comments))
		for _, c := range res.Comments {
			comments = append(comments, changeComment{
				Change: c.Change,
				Body:   c.Body,
			})
		}
		slices.SortFunc(comments, func(a, b changeComment) int {
			if a.Change != b.Change {
				return a.Change - b.Change
			}
			return strings.Compare(a.Body, b.Body)
		})
		enc := yaml.NewEncoder(c.stdout)
		enc.SetIndent(2)
		return enc.Encode(comments)

	case "change":
		if len(args) != 2 {
			return errors.New("usage: shamhub dump change <N>")
		}
		change, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid change number %q: %w", args[1], err)
		}

		var res adminDumpChangeResponse
		if err := c.client.Get(
			c.ctx,
			"/_shamhub/admin/dump/changes/"+strconv.Itoa(change),
			&res,
		); err != nil {
			return err
		}
		return encodeJSON(c.stdout, res.Change)

	default:
		return fmt.Errorf("unknown dump command: %s", args[0])
	}
}

func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func parseOwnerRepo(ownerRepo string) (owner string, repo string, err error) {
	owner, repo, ok := strings.Cut(ownerRepo, "/")
	if !ok {
		return "", "", fmt.Errorf("invalid owner/repo: %s", ownerRepo)
	}
	return owner, strings.TrimSuffix(repo, ".git"), nil
}

func adminChangePath(owner string, repo string, number int, action string) string {
	return fmt.Sprintf(
		"/_shamhub/admin/changes/%s/%s/%d/%s",
		url.PathEscape(owner),
		url.PathEscape(repo),
		number,
		action,
	)
}

// runGit invokes git for commands that intentionally mirror repository setup.
func (c *shamhubCLI) runGit(args ...string) error {
	if err := xec.Command(c.ctx, nil, "git", args...).
		WithStderr(c.stderr).
		Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// nullableBoolFlag records whether a boolean flag was absent or set.
type nullableBoolFlag struct {
	value *bool
}

func (f *nullableBoolFlag) String() string {
	if f.value == nil {
		return ""
	}
	return strconv.FormatBool(*f.value)
}

func (f *nullableBoolFlag) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	f.value = &v
	return nil
}

func (f *nullableBoolFlag) Ptr() *bool {
	return f.value
}

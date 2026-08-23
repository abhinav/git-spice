package shamhub

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/forge"
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
	case "review":
		return c.review(args)
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

// Review commands seed feedback submissions and review threads for tests.
func (c *shamhubCLI) review(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: shamhub review <submit|comment> [args ...]")
	}

	switch args[0] {
	case "submit":
		return c.submitReview(args[1:])
	case "comment":
		return c.reviewComment(args[1:])
	default:
		return fmt.Errorf("unknown shamhub review command: %s", args[0])
	}
}

func (c *shamhubCLI) submitReview(args []string) error {
	flags := flag.NewFlagSet("shamhub review submit", flag.ContinueOnError)
	flags.SetOutput(c.stderr)
	reviewer := flags.String("reviewer", "reviewer", "reviewer username")
	disposition := flags.String("disposition", "comment", "review disposition")
	body := flags.String("body", "", "review body")
	if err := flags.Parse(args); err != nil {
		return err
	}
	args = flags.Args()
	if len(args) != 2 {
		return errors.New(
			"usage: shamhub review submit " +
				"[--reviewer <username>] " +
				"[--disposition comment|approve|request-changes] " +
				"[--body <body>] <owner/repo> <change>",
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

	time, err := c.committerTime()
	if err != nil {
		return err
	}
	dispositionValue, err := parseReviewDisposition(*disposition)
	if err != nil {
		return err
	}
	return c.client.Post(c.ctx, "/_shamhub/admin/reviews", adminSubmitFeedbackBody{
		Owner:       owner,
		Repo:        repo,
		Change:      change,
		Submitter:   *reviewer,
		Disposition: dispositionValue,
		Body:        *body,
		Time:        time,
	}, &adminSubmitFeedbackResponse{})
}

func parseReviewDisposition(name string) (int, error) {
	switch name {
	case "comment":
		return int(forge.ReviewDispositionNone), nil
	case "approve":
		return int(forge.ReviewDispositionApprove), nil
	case "request-changes":
		return int(forge.ReviewDispositionRequestChanges), nil
	default:
		return 0, fmt.Errorf("invalid review disposition %q", name)
	}
}

func (c *shamhubCLI) reviewComment(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: shamhub review comment <post|reply> [args ...]")
	}

	switch args[0] {
	case "post":
		return c.postReviewComment(args[1:])
	case "reply":
		return c.replyReviewComment(args[1:])
	default:
		return fmt.Errorf("unknown shamhub review comment command: %s", args[0])
	}
}

func (c *shamhubCLI) postReviewComment(args []string) error {
	flags := flag.NewFlagSet("shamhub review comment post", flag.ContinueOnError)
	flags.SetOutput(c.stderr)
	id := flags.Int("id", 0, "explicit comment ID")
	author := flags.String("author", "reviewer", "comment author")
	path := flags.String("path", "", "file path")
	rangeValue := flags.String("range", "", "inclusive line range")
	side := flags.String("side", "", "diff side")
	resolved := flags.Bool("resolved", false, "mark thread resolved")
	outdated := flags.Bool("outdated", false, "mark thread outdated")
	if err := flags.Parse(args); err != nil {
		return err
	}
	args = flags.Args()
	if len(args) != 3 {
		return errors.New(
			"usage: shamhub review comment post " +
				"[--id <id>] [--author <username>] --path <path> " +
				"[--range <start[:end]>] [--side left|right] [--resolved] " +
				"[--outdated] <owner/repo> <change> <body>",
		)
	}

	var start, end, sideValue int
	if *rangeValue == "" {
		if *side != "" {
			return errors.New("--side requires --range")
		}
	} else {
		var err error
		start, end, err = parseReviewRange(*rangeValue)
		if err != nil {
			return err
		}
		sideName := *side
		if sideName == "" {
			sideName = "right"
		}
		sideValue, err = parseReviewSide(sideName)
		if err != nil {
			return err
		}
	}
	owner, repo, err := parseOwnerRepo(args[0])
	if err != nil {
		return err
	}
	change, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("invalid change number %q: %w", args[1], err)
	}

	time, err := c.committerTime()
	if err != nil {
		return err
	}
	var response adminPostReviewCommentResponse
	if err := c.client.Post(c.ctx, "/_shamhub/admin/review-comments", adminPostReviewCommentBody{
		Owner:      owner,
		Repo:       repo,
		Change:     change,
		ID:         *id,
		Author:     *author,
		Path:       *path,
		RangeStart: start,
		RangeEnd:   end,
		Side:       sideValue,
		Body:       args[2],
		Resolved:   *resolved,
		Outdated:   *outdated,
		Time:       time,
	}, &response); err != nil {
		return err
	}
	fmt.Fprintln(c.stdout, response.ID)
	return nil
}

func (c *shamhubCLI) replyReviewComment(args []string) error {
	flags := flag.NewFlagSet("shamhub review comment reply", flag.ContinueOnError)
	flags.SetOutput(c.stderr)
	id := flags.Int("id", 0, "explicit comment ID")
	author := flags.String("author", "reviewer", "comment author")
	if err := flags.Parse(args); err != nil {
		return err
	}
	args = flags.Args()
	if len(args) != 4 {
		return errors.New(
			"usage: shamhub review comment reply " +
				"[--id <id>] [--author <username>] " +
				"<owner/repo> <change> <thread-id> <body>",
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

	time, err := c.committerTime()
	if err != nil {
		return err
	}
	var response adminPostReviewCommentResponse
	if err := c.client.Post(c.ctx, "/_shamhub/admin/review-comments", adminPostReviewCommentBody{
		Owner:    owner,
		Repo:     repo,
		Change:   change,
		ID:       *id,
		Author:   *author,
		ThreadID: args[2],
		Body:     args[3],
		Time:     time,
	}, &response); err != nil {
		return err
	}
	fmt.Fprintln(c.stdout, response.ID)
	return nil
}

func parseReviewRange(value string) (start, end int, _ error) {
	startText, endText, hasEnd := strings.Cut(value, ":")
	start, err := strconv.Atoi(startText)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid review range %q: %w", value, err)
	}
	end = start
	if hasEnd {
		end, err = strconv.Atoi(endText)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid review range %q: %w", value, err)
		}
	}
	if err := validateReviewRange(forge.ReviewThreadRange{StartLine: start, EndLine: end}); err != nil {
		return 0, 0, fmt.Errorf("invalid review range %q: %w", value, err)
	}
	return start, end, nil
}

func parseReviewSide(name string) (int, error) {
	switch name {
	case "left":
		return int(forge.ReviewThreadSideLeft), nil
	case "right":
		return int(forge.ReviewThreadSideRight), nil
	default:
		return 0, fmt.Errorf("invalid review side %q", name)
	}
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
	if req.Time, err = c.committerTime(); err != nil {
		return err
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

	case "reviews":
		u := "/_shamhub/admin/dump/reviews"
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

		var res adminDumpFeedbackResponse
		if err := c.client.Get(c.ctx, u, &res); err != nil {
			return err
		}
		enc := yaml.NewEncoder(c.stdout)
		enc.SetIndent(2)
		return enc.Encode(res)

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
	enc := jsontext.NewEncoder(
		w,
		jsontext.EscapeForHTML(false),
		jsontext.WithIndent("  "),
	)
	return json.MarshalEncode(enc, v)
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

func (c *shamhubCLI) committerTime() (time.Time, error) {
	at := c.getenv("GIT_COMMITTER_DATE")
	if at == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid GIT_COMMITTER_DATE: %w", err)
	}
	return t, nil
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

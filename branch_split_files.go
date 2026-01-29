package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget"
)

type branchSplitFilesCmd struct {
	Branch string `arg:"" optional:"" help:"Branch to split (default: current branch)"`

	// Non-interactive mode for scripting/testing.
	// Format: "file1|file2:branch-name:commit-msg" (pipe-separated files).
	At []splitFilesPoint `name:"at" help:"Non-interactive: FILE|FILE:BRANCH:MESSAGE (repeatable)"`

	// What to do with original branch in non-interactive mode.
	Keep   bool `help:"Keep original branch at top of stack (non-interactive)"`
	Delete bool `help:"Delete original branch (non-interactive)"`
}

// splitFilesPoint represents a file group specification for splitting.
type splitFilesPoint struct {
	Files   []string // pipe-separated file paths
	Branch  string   // branch name
	Message string   // commit message
}

// Decode parses a FILES:BRANCH:MESSAGE specification.
func (p *splitFilesPoint) Decode(ctx *kong.DecodeContext) error {
	var spec string
	if err := ctx.Scan.PopValueInto("at", &spec); err != nil {
		return err
	}

	// Split by last two colons to get files:branch:message.
	lastColon := strings.LastIndex(spec, ":")
	if lastColon == -1 {
		return fmt.Errorf("expected FILES:BRANCH:MESSAGE, got %q", spec)
	}
	p.Message = spec[lastColon+1:]
	spec = spec[:lastColon]

	lastColon = strings.LastIndex(spec, ":")
	if lastColon == -1 {
		return errors.New("expected FILES:BRANCH:MESSAGE, got incomplete spec")
	}
	p.Branch = spec[lastColon+1:]
	filesStr := spec[:lastColon]

	if filesStr == "" {
		return errors.New("file list cannot be empty")
	}
	if p.Branch == "" {
		return errors.New("branch name cannot be empty")
	}

	// Split files by pipe.
	p.Files = strings.Split(filesStr, "|")
	return nil
}

func (*branchSplitFilesCmd) Help() string {
	return text.Dedent(`
		Splits the file changes in a branch into a linear stack of branches.
		Each new branch contains a squashed commit with the selected files.

		Unlike 'gs branch split', which splits at commit boundaries,
		this command splits by files: all file changes in the branch
		(compared to its base) are presented for selection.

		Use --at to specify splits non-interactively:

			gs branch split-files \
			  --at "src/api.go|src/routes.go:api-changes:Add API" \
			  --at "src/db.go:db-changes:Add DB queries" \
			  --keep

		Files are separated by pipes (|), followed by branch name and message.
		Use --keep to keep the original branch at the top of the stack,
		or --delete to remove it after splitting.
	`)
}

func (cmd *branchSplitFilesCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	restackHandler RestackHandler,
) (err error) {
	// Determine which branch to split.
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil && !errors.Is(err, git.ErrDetachedHead) {
		return fmt.Errorf("get current branch: %w", err)
	}

	targetBranch := cmd.Branch
	if targetBranch == "" {
		if currentBranch == "" {
			return errors.New("not on a branch; specify a branch to split")
		}
		targetBranch = currentBranch
	}

	// Validate target branch.
	if targetBranch == store.Trunk() {
		return errors.New("cannot split trunk branch")
	}

	branchInfo, err := svc.LookupBranch(ctx, targetBranch)
	if err != nil {
		return fmt.Errorf("lookup branch %q: %w", targetBranch, err)
	}

	// Check for clean working tree.
	if err := cmd.ensureCleanWorkTree(ctx, wt); err != nil {
		return err
	}

	// Warn about upstack branches.
	aboves, err := svc.ListAbove(ctx, targetBranch)
	if err != nil {
		return fmt.Errorf("list branches above %q: %w", targetBranch, err)
	}
	if len(aboves) > 0 && ui.Interactive(view) {
		log.Warnf("Branch %q has upstack branches: %v", targetBranch, aboves)
		log.Warn("This operation will rewrite history.")

		var confirm bool
		prompt := ui.NewConfirm().
			WithTitle("Continue with split?").
			WithDescription("Upstack branches will be restacked.").
			WithValue(&confirm)
		if err := ui.Run(view, prompt); err != nil {
			return fmt.Errorf("prompt: %w", err)
		}
		if !confirm {
			return errors.New("operation cancelled")
		}
	}

	// Compute the actual merge-base with the base branch.
	// We use the merge-base rather than the stored BaseHash because
	// the stored hash may be outdated if the branch has been modified.
	mergeBase, err := repo.MergeBase(ctx, branchInfo.Base, targetBranch)
	if err != nil {
		return fmt.Errorf("compute merge-base: %w", err)
	}

	// Get all changed files between branch and its base.
	files, err := cmd.getChangedFiles(ctx, repo, mergeBase, branchInfo.Head)
	if err != nil {
		return fmt.Errorf("get changed files: %w", err)
	}

	if len(files) == 0 {
		return errors.New("no files changed in branch")
	}
	if len(files) == 1 {
		return errors.New("only one file changed; nothing to split")
	}

	// Collect file groups for splitting.
	var groups []fileGroup
	if len(cmd.At) > 0 {
		// Non-interactive mode.
		groups, err = cmd.parseNonInteractiveGroups(ctx, repo, targetBranch, files)
		if err != nil {
			return err
		}
	} else {
		// Interactive mode.
		groups, err = cmd.selectFilesInteractively(ctx, view, targetBranch, files)
		if err != nil {
			return err
		}
	}

	if len(groups) == 0 {
		return errors.New("no file groups selected")
	}

	// Execute the split.
	result, err := cmd.executeSplit(ctx, log, repo, wt, store, svc, targetBranch, branchInfo, groups)
	if err != nil {
		return err
	}

	// Restack upstack branches onto the top branch.
	if len(aboves) > 0 {
		log.Info("Restacking upstack branches...")
		if err := restackHandler.RestackUpstack(ctx, result.topBranch, &restack.UpstackOptions{
			SkipStart: true,
		}); err != nil {
			return fmt.Errorf("restack upstack: %w", err)
		}
	}

	// Checkout the top branch if we're on the original branch.
	if currentBranch == targetBranch && result.topBranch != targetBranch {
		if err := wt.CheckoutBranch(ctx, result.topBranch); err != nil {
			return fmt.Errorf("checkout %q: %w", result.topBranch, err)
		}
	}

	log.Infof("Split complete. Stack: %s", strings.Join(result.stack, " → "))
	return nil
}

// fileGroup represents a group of files to be put into a single branch.
type fileGroup struct {
	files   []widget.FileEntry
	branch  string
	message string
}

// splitResult contains the result of a split operation.
type splitResult struct {
	topBranch string   // name of the topmost branch after split
	stack     []string // ordered list of branches (bottom to top)
}

func (cmd *branchSplitFilesCmd) ensureCleanWorkTree(ctx context.Context, wt *git.Worktree) error {
	// Check for staged changes.
	staged, err := wt.DiffIndex(ctx, "HEAD")
	if err != nil {
		return fmt.Errorf("check staged changes: %w", err)
	}
	if len(staged) > 0 {
		return errors.New("working tree has staged changes; commit or stash them first")
	}

	// Check for unstaged changes.
	var hasUnstaged bool
	for _, err := range wt.DiffWork(ctx) {
		if err != nil {
			return fmt.Errorf("check unstaged changes: %w", err)
		}
		hasUnstaged = true
		break
	}
	if hasUnstaged {
		return errors.New("working tree has unstaged changes; commit or stash them first")
	}

	return nil
}

func (cmd *branchSplitFilesCmd) getChangedFiles(
	ctx context.Context,
	repo *git.Repository,
	baseHash, headHash git.Hash,
) ([]widget.FileEntry, error) {
	var files []widget.FileEntry
	for status, err := range repo.DiffTree(ctx, baseHash.String(), headHash.String()) {
		if err != nil {
			return nil, err
		}

		entry := widget.FileEntry{
			Status: status.Status,
			Path:   status.Path,
		}

		// Handle renames: status starts with R and may have a score (e.g., R100).
		if strings.HasPrefix(status.Status, "R") {
			entry.Status = "R"
			// For renames, DiffTree returns the path as "old\tnew".
			if oldPath, newPath, ok := strings.Cut(status.Path, "\t"); ok {
				entry.OldPath = oldPath
				entry.Path = newPath
			}
		}

		files = append(files, entry)
	}
	return files, nil
}

func (cmd *branchSplitFilesCmd) parseNonInteractiveGroups(
	ctx context.Context,
	repo *git.Repository,
	originalBranch string,
	allFiles []widget.FileEntry,
) ([]fileGroup, error) {
	// Build a map of file paths to entries for quick lookup.
	fileMap := make(map[string]widget.FileEntry, len(allFiles))
	for _, f := range allFiles {
		fileMap[f.Path] = f
		if f.OldPath != "" {
			// Also allow selection by old path for renames.
			fileMap[f.OldPath] = f
		}
	}

	usedFiles := make(map[string]bool)
	var groups []fileGroup

	for i, at := range cmd.At {
		var groupFiles []widget.FileEntry
		for _, filePath := range at.Files {
			entry, ok := fileMap[filePath]
			if !ok {
				return nil, fmt.Errorf("--at[%d]: file %q not found in branch changes", i, filePath)
			}
			if usedFiles[entry.Path] {
				return nil, fmt.Errorf("--at[%d]: file %q already assigned to another group", i, filePath)
			}
			usedFiles[entry.Path] = true
			groupFiles = append(groupFiles, entry)
		}

		// Validate branch name.
		if at.Branch != originalBranch && repo.BranchExists(ctx, at.Branch) {
			return nil, fmt.Errorf("--at[%d]: branch %q already exists", i, at.Branch)
		}

		groups = append(groups, fileGroup{
			files:   groupFiles,
			branch:  at.Branch,
			message: at.Message,
		})
	}

	// Check if all files are assigned when --delete is used.
	if cmd.Delete {
		for _, f := range allFiles {
			if !usedFiles[f.Path] {
				return nil, fmt.Errorf(
					"--delete requires all files to be assigned; file %q is unassigned",
					f.Path,
				)
			}
		}
	}

	// If --keep, remaining files go to the original branch.
	if cmd.Keep {
		var remaining []widget.FileEntry
		for _, f := range allFiles {
			if !usedFiles[f.Path] {
				remaining = append(remaining, f)
			}
		}
		if len(remaining) > 0 {
			groups = append(groups, fileGroup{
				files:   remaining,
				branch:  originalBranch,
				message: "", // Will use original commit message.
			})
		}
	}

	return groups, nil
}

func (cmd *branchSplitFilesCmd) selectFilesInteractively(
	_ context.Context,
	view ui.View,
	originalBranch string,
	allFiles []widget.FileEntry,
) ([]fileGroup, error) {
	if !ui.Interactive(view) {
		return nil, fmt.Errorf("use --at to split non-interactively: %w", ui.ErrPrompt)
	}

	remainingFiles := slices.Clone(allFiles)
	var groups []fileGroup
	usedBranchNames := make(map[string]bool)

	groupNum := 1
	for len(remainingFiles) > 0 {
		// Prompt for file selection.
		selectWidget := widget.NewFileSelect().
			WithTitle(fmt.Sprintf("Select files for branch %d", groupNum)).
			WithDescription("Select files to include in this branch (space to toggle, enter to confirm)").
			WithFiles(remainingFiles)

		if err := ui.Run(view, selectWidget); err != nil {
			return nil, fmt.Errorf("select files: %w", err)
		}

		selectedFiles := selectWidget.SelectedFiles()
		if len(selectedFiles) == 0 {
			// User selected nothing - done with grouping.
			break
		}

		// Prompt for branch name.
		suggestedName := spice.GenerateBranchName(selectedFiles[0].Path, 32)
		var branchName string
		branchInput := ui.NewInput().
			WithTitle("Branch name").
			WithDescription(fmt.Sprintf("Enter name for branch containing %d files", len(selectedFiles))).
			WithValue(&branchName).
			WithValidate(func(name string) error {
				name = strings.TrimSpace(name)
				if name == "" {
					return errors.New("branch name cannot be empty")
				}
				if usedBranchNames[name] {
					return fmt.Errorf("branch name %q already used in this split", name)
				}
				return nil
			}).
			WithOptions([]string{suggestedName})
		branchName = suggestedName

		if err := ui.Run(view, branchInput); err != nil {
			return nil, fmt.Errorf("branch name: %w", err)
		}
		branchName = strings.TrimSpace(branchName)

		// Prompt for commit message.
		var commitMessage string
		messageInput := ui.NewInput().
			WithTitle("Commit message").
			WithDescription("Enter commit message (press enter to use branch name)").
			WithValue(&commitMessage).
			WithOptions([]string{branchName})
		commitMessage = branchName

		if err := ui.Run(view, messageInput); err != nil {
			return nil, fmt.Errorf("commit message: %w", err)
		}
		if commitMessage == "" {
			commitMessage = branchName
		}

		// Record the group.
		usedBranchNames[branchName] = true
		groups = append(groups, fileGroup{
			files:   selectedFiles,
			branch:  branchName,
			message: commitMessage,
		})

		// Remove selected files from remaining.
		selectedSet := make(map[string]bool)
		for _, f := range selectedFiles {
			selectedSet[f.Path] = true
		}
		var newRemaining []widget.FileEntry
		for _, f := range remainingFiles {
			if !selectedSet[f.Path] {
				newRemaining = append(newRemaining, f)
			}
		}
		remainingFiles = newRemaining
		groupNum++
	}

	// Handle remaining files.
	if len(remainingFiles) > 0 {
		var keepOriginal bool
		prompt := ui.NewConfirm().
			WithTitle(fmt.Sprintf("Keep %q at top of stack?", originalBranch)).
			WithDescription(fmt.Sprintf("%d remaining files will be committed to it.", len(remainingFiles))).
			WithValue(&keepOriginal)
		if err := ui.Run(view, prompt); err != nil {
			return nil, fmt.Errorf("prompt: %w", err)
		}

		if keepOriginal {
			groups = append(groups, fileGroup{
				files:   remainingFiles,
				branch:  originalBranch,
				message: "", // Will reuse original commit message.
			})
		} else {
			return nil, errors.New("all files must be assigned when not keeping original branch")
		}
	}

	return groups, nil
}

func (cmd *branchSplitFilesCmd) executeSplit(
	ctx context.Context,
	log *silog.Logger,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	originalBranch string,
	branchInfo *spice.LookupBranchResponse,
	groups []fileGroup,
) (*splitResult, error) {
	// Track state for rollback.
	rollbackState := &splitState{
		originalBranch: originalBranch,
		originalHead:   branchInfo.Head,
		originalBase:   branchInfo.Base,
	}

	defer func() {
		if rollbackState.needsRollback {
			rollbackState.rollback(ctx, log, repo, wt, store)
		}
	}()
	rollbackState.needsRollback = true

	// Start transaction.
	branchTx := store.BeginBranchTx()

	baseRef := branchInfo.Base
	baseHash := branchInfo.BaseHash
	var stack []string
	stack = append(stack, baseRef)

	// Checkout base to start clean.
	if err := wt.DetachHead(ctx, baseHash.String()); err != nil {
		return nil, fmt.Errorf("detach head at base: %w", err)
	}

	for _, group := range groups {
		log.Debugf("Creating branch %q with %d files", group.branch, len(group.files))

		// Apply file changes.
		for _, file := range group.files {
			if err := cmd.applyFileChange(ctx, wt, originalBranch, file); err != nil {
				return nil, fmt.Errorf("apply file %q: %w", file.Path, err)
			}
		}

		// Determine commit message.
		message := group.message
		if message == "" {
			// Reuse original commit message.
			commitObj, err := repo.ReadCommit(ctx, branchInfo.Head.String())
			if err != nil {
				return nil, fmt.Errorf("read original commit: %w", err)
			}
			message = commitObj.Message()
		}

		// Create commit.
		if err := wt.Commit(ctx, git.CommitRequest{
			Message: message,
		}); err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}

		// Get new commit hash.
		newHead, err := wt.Head(ctx)
		if err != nil {
			return nil, fmt.Errorf("get new head: %w", err)
		}

		// Handle branch creation/update.
		isOriginalBranch := group.branch == originalBranch
		if isOriginalBranch {
			// Move original branch pointer.
			if err := repo.SetRef(ctx, git.SetRefRequest{
				Ref:     "refs/heads/" + originalBranch,
				Hash:    newHead,
				OldHash: branchInfo.Head,
				Reason:  "gs: split-files move original branch",
			}); err != nil {
				return nil, fmt.Errorf("move branch %q: %w", originalBranch, err)
			}
			// For subsequent SetRef calls, use the new hash.
			branchInfo.Head = newHead

			// Update tracking.
			if err := branchTx.Upsert(ctx, state.UpsertRequest{
				Name:     originalBranch,
				Base:     baseRef,
				BaseHash: baseHash,
			}); err != nil {
				return nil, fmt.Errorf("update tracking for %q: %w", originalBranch, err)
			}
		} else {
			// Create new branch.
			if err := repo.CreateBranch(ctx, git.CreateBranchRequest{
				Name: group.branch,
				Head: newHead.String(),
			}); err != nil {
				return nil, fmt.Errorf("create branch %q: %w", group.branch, err)
			}
			rollbackState.createdBranches = append(rollbackState.createdBranches, group.branch)

			// Track new branch.
			if err := branchTx.Upsert(ctx, state.UpsertRequest{
				Name:     group.branch,
				Base:     baseRef,
				BaseHash: baseHash,
			}); err != nil {
				return nil, fmt.Errorf("track branch %q: %w", group.branch, err)
			}
		}

		// Update base for next group.
		baseRef = group.branch
		baseHash = newHead
		stack = append(stack, group.branch)
	}

	// Determine top branch.
	topBranch := groups[len(groups)-1].branch

	// Check if original branch needs to be deleted.
	originalUsed := false
	for _, g := range groups {
		if g.branch == originalBranch {
			originalUsed = true
			break
		}
	}

	if !originalUsed {
		// Original branch was not used - delete it.
		if err := svc.ForgetBranch(ctx, originalBranch); err != nil {
			return nil, fmt.Errorf("forget branch %q: %w", originalBranch, err)
		}
		if err := repo.DeleteBranch(ctx, originalBranch, git.BranchDeleteOptions{Force: true}); err != nil {
			return nil, fmt.Errorf("delete branch %q: %w", originalBranch, err)
		}
		log.Infof("Deleted branch %q", originalBranch)
	}

	// Commit the transaction.
	if err := branchTx.Commit(ctx, fmt.Sprintf("%s: split into %d branches", originalBranch, len(groups))); err != nil {
		return nil, fmt.Errorf("commit state: %w", err)
	}

	// Checkout the top branch.
	if err := wt.CheckoutBranch(ctx, topBranch); err != nil {
		return nil, fmt.Errorf("checkout top branch %q: %w", topBranch, err)
	}

	rollbackState.needsRollback = false

	return &splitResult{
		topBranch: topBranch,
		stack:     stack,
	}, nil
}

func (cmd *branchSplitFilesCmd) applyFileChange(
	ctx context.Context,
	wt *git.Worktree,
	sourceBranch string,
	file widget.FileEntry,
) error {
	switch file.Status {
	case "A", "M":
		// Added or modified: checkout from source and stage.
		if err := wt.CheckoutFiles(ctx, &git.CheckoutFilesRequest{
			TreeIsh:   sourceBranch,
			Pathspecs: []string{file.Path},
			Overlay:   true,
		}); err != nil {
			return fmt.Errorf("checkout file: %w", err)
		}
		// Stage the file using git add.
		if err := wt.Reset(ctx, sourceBranch, git.ResetOptions{
			Paths: []string{file.Path},
		}); err != nil {
			return fmt.Errorf("stage file: %w", err)
		}

	case "D":
		// Deleted: remove from index.
		// First check if the file exists in the current index.
		if err := wt.Reset(ctx, sourceBranch, git.ResetOptions{
			Paths: []string{file.Path},
		}); err != nil {
			return fmt.Errorf("stage deletion: %w", err)
		}

	case "R":
		// Renamed: remove old path, add new path.
		if file.OldPath != "" {
			// Stage the removal of the old path.
			if err := wt.Reset(ctx, sourceBranch, git.ResetOptions{
				Paths: []string{file.OldPath},
			}); err != nil {
				// Ignore error if old path doesn't exist in current tree.
				_ = err
			}
		}
		// Checkout and stage the new path.
		if err := wt.CheckoutFiles(ctx, &git.CheckoutFilesRequest{
			TreeIsh:   sourceBranch,
			Pathspecs: []string{file.Path},
			Overlay:   true,
		}); err != nil {
			return fmt.Errorf("checkout renamed file: %w", err)
		}
		if err := wt.Reset(ctx, sourceBranch, git.ResetOptions{
			Paths: []string{file.Path},
		}); err != nil {
			return fmt.Errorf("stage renamed file: %w", err)
		}

	default:
		return fmt.Errorf("unsupported file status: %s", file.Status)
	}

	return nil
}

// splitState tracks state for rollback on failure.
type splitState struct {
	originalBranch  string
	originalHead    git.Hash
	originalBase    string
	createdBranches []string
	needsRollback   bool
}

func (s *splitState) rollback(
	ctx context.Context,
	log *silog.Logger,
	repo *git.Repository,
	wt *git.Worktree,
	_ *state.Store,
) {
	ctx = context.WithoutCancel(ctx)
	log.Warn("Rolling back split operation...")

	// Delete created branches.
	for _, branch := range s.createdBranches {
		if err := repo.DeleteBranch(ctx, branch, git.BranchDeleteOptions{Force: true}); err != nil {
			log.Warnf("Failed to delete branch %q: %v", branch, err)
		}
	}

	// Restore original branch pointer.
	if err := repo.SetRef(ctx, git.SetRefRequest{
		Ref:    "refs/heads/" + s.originalBranch,
		Hash:   s.originalHead,
		Reason: "gs: rollback split-files",
	}); err != nil {
		log.Warnf("Failed to restore branch %q: %v", s.originalBranch, err)
	}

	// Checkout original branch.
	if err := wt.CheckoutBranch(ctx, s.originalBranch); err != nil {
		log.Warnf("Failed to checkout %q: %v", s.originalBranch, err)
	}

	log.Warn("Rollback complete. Original branch restored.")
}

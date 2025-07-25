package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/komplete"
)

type shellCompletionCmd struct {
	*komplete.Command `embed:""`
}

func (c *shellCompletionCmd) Help() string {
	return text.Dedent(`
		To set up shell completion, eval the output of this command
		from your shell's rc file.
		For example:

			# bash
			eval "$(gs shell completion bash)"

			# zsh
			eval "$(gs shell completion zsh)"

			# fish
			eval "$(gs shell completion fish)"

		If shell name is not provided, the current shell is guessed
		using a heuristic.
	`)
}

func predictBranches(_ komplete.Args) (predictions []string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	repo, err := git.Open(ctx, ".", git.OpenOptions{})
	if err != nil {
		return nil
	}

	branches, err := sliceutil.CollectErr(repo.LocalBranches(ctx, nil))
	if err != nil {
		return nil
	}

	for _, branch := range branches {
		predictions = append(predictions, branch.Name)
	}

	return predictions
}

func predictTrackedBranches(_ komplete.Args) (predictions []string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	repo, err := git.Open(ctx, ".", git.OpenOptions{})
	if err != nil {
		return nil
	}

	db := newRepoStorage(repo, nil /* log */)
	store, err := state.OpenStore(ctx, db, nil /* log */)
	if err != nil {
		return nil // not initialized
	}

	branches, err := sliceutil.CollectErr(store.ListBranches(ctx))
	if err != nil {
		return nil
	}
	sort.Strings(branches)

	return branches
}

func predictRemotes(_ komplete.Args) (predictions []string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	repo, err := git.Open(ctx, ".", git.OpenOptions{})
	if err != nil {
		return nil
	}

	remotes, err := repo.ListRemotes(ctx)
	if err != nil {
		return nil
	}

	return remotes
}

func predictDirs(args komplete.Args) (predictions []string) {
	dir, last := filepath.Split(args.Last)
	dir = filepath.Clean(dir)

	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	sep := string(filepath.Separator)

	for _, ent := range ents {
		if !ent.IsDir() || strings.HasPrefix(ent.Name(), ".") {
			continue
		}

		if strings.HasPrefix(ent.Name(), last) {
			name := filepath.Join(dir, ent.Name())
			if !strings.HasSuffix(name, sep) {
				name += sep
			}

			predictions = append(predictions, name)
		}
	}

	return predictions
}

func predictForges(forges *forge.Registry) func(komplete.Args) (predictions []string) {
	return func(komplete.Args) (predictions []string) {
		var ids []string
		for f := range forges.All() {
			ids = append(ids, f.ID())
		}
		sort.Strings(ids)
		return ids
	}
}

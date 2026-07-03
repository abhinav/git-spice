package gitea

import (
	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
	"go.abhg.dev/gs/internal/git"
)

func pullRequestToChangeStatus(pr *giteagw.PullRequest) forge.ChangeStatus {
	return forge.ChangeStatus{
		State:    toChangeState(pr),
		HeadHash: headHash(pr),
	}
}

func toChangeState(pr *giteagw.PullRequest) forge.ChangeState {
	if pr.Merged {
		return forge.ChangeMerged
	}
	switch pr.State {
	case "open":
		return forge.ChangeOpen
	case "closed":
		return forge.ChangeClosed
	default:
		return forge.ChangeOpen
	}
}

func headHash(pr *giteagw.PullRequest) git.Hash {
	if pr.Head == nil {
		return ""
	}
	return git.Hash(pr.Head.Sha)
}

func pullRequestToFindChangeItem(pr *giteagw.PullRequest) *forge.FindChangeItem {
	var labelNames []string
	if len(pr.Labels) > 0 {
		labelNames = make([]string, len(pr.Labels))
		for i, l := range pr.Labels {
			labelNames[i] = l.Name
		}
	}

	var reviewers []string
	if len(pr.RequestedReviewers) > 0 {
		reviewers = make([]string, len(pr.RequestedReviewers))
		for i, u := range pr.RequestedReviewers {
			reviewers[i] = u.Login
		}
	}

	var assignees []string
	if len(pr.Assignees) > 0 {
		assignees = make([]string, len(pr.Assignees))
		for i, u := range pr.Assignees {
			assignees[i] = u.Login
		}
	}

	var baseRef string
	if pr.Base != nil {
		baseRef = pr.Base.Ref
	}

	// Strip the "WIP:" prefix from the title if present.
	// Gitea stores draft state as a title prefix; the Subject
	// should be the clean title without the prefix.
	title := _draftRegex.ReplaceAllString(pr.Title, "")

	return &forge.FindChangeItem{
		ID:        &PR{Number: pr.Number},
		URL:       pr.HTMLURL,
		State:     toChangeState(pr),
		Subject:   title,
		BaseName:  baseRef,
		HeadHash:  headHash(pr),
		Draft:     pr.Draft,
		Labels:    labelNames,
		Reviewers: reviewers,
		Assignees: assignees,
	}
}

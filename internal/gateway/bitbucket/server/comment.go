package server

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

// CreateComment posts a new top-level comment on a pull request,
// capturing the comment's initial optimistic-locking version.
func (g *Gateway) CreateComment(
	ctx context.Context,
	prID int64,
	body string,
) (*bitbucket.ChangeComment, error) {
	comment, _, err := g.client.CommentCreate(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, prID, body,
	)
	if err != nil {
		return nil, fmt.Errorf("create comment: %w", err)
	}

	g.log.Debug("Posted comment", "pr", prID, "comment", comment.ID)
	return &bitbucket.ChangeComment{
		ID:      comment.ID,
		PRID:    prID,
		Version: comment.Version,
		Body:    comment.Text,
	}, nil
}

// UpdateComment replaces the body of an existing pull request comment.
//
// A stale optimistic-locking version ([ErrConflict]) triggers
// one refetch of the live version and a single retry. A comment that no longer
// exists yields [forge.ErrNotFound] so the caller can recreate it.
func (g *Gateway) UpdateComment(
	ctx context.Context,
	c *bitbucket.ChangeComment,
	body string,
) error {
	_, _, err := g.client.CommentUpdate(
		ctx, g.repoID.restProjectKey(), g.repoID.slug,
		c.PRID, c.ID, body, c.Version,
	)

	if errors.Is(err, ErrConflict) {
		g.log.Debug("Comment version conflict; refetching and retrying",
			"pr", c.PRID, "comment", c.ID)
		version, found, ferr := g.liveCommentVersion(ctx, c.PRID, c.ID)
		if ferr != nil {
			return ferr
		}
		if !found {
			return fmt.Errorf("comment %d not found: %w", c.ID, forge.ErrNotFound)
		}
		_, _, err = g.client.CommentUpdate(
			ctx, g.repoID.restProjectKey(), g.repoID.slug,
			c.PRID, c.ID, body, version,
		)
	}

	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("comment %d not found: %w", c.ID, forge.ErrNotFound)
		}
		return fmt.Errorf("update comment: %w", err)
	}

	return nil
}

// DeleteComment deletes a pull request comment.
//
// Like UpdateComment, a stale version triggers one refetch and retry;
// an already-deleted comment is treated as success.
func (g *Gateway) DeleteComment(
	ctx context.Context,
	c *bitbucket.ChangeComment,
) error {
	_, err := g.client.CommentDelete(
		ctx, g.repoID.restProjectKey(), g.repoID.slug,
		c.PRID, c.ID, c.Version,
	)

	if errors.Is(err, ErrConflict) {
		g.log.Debug("Comment version conflict; refetching and retrying",
			"pr", c.PRID, "comment", c.ID)
		version, found, ferr := g.liveCommentVersion(ctx, c.PRID, c.ID)
		if ferr != nil {
			return ferr
		}
		if !found {
			// Already gone; nothing to delete.
			return nil
		}
		_, err = g.client.CommentDelete(
			ctx, g.repoID.restProjectKey(), g.repoID.slug,
			c.PRID, c.ID, version,
		)
	}

	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Already deleted; treat as success.
			return nil
		}
		return fmt.Errorf("delete comment: %w", err)
	}

	return nil
}

// liveCommentVersion recovers a comment's current optimistic-locking version
// from the pull request activity feed. found is false if the comment is
// absent (deleted).
func (g *Gateway) liveCommentVersion(
	ctx context.Context,
	prID, commentID int64,
) (version int, found bool, err error) {
	for activity, aerr := range g.client.ActivityList(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, prID,
	) {
		if aerr != nil {
			return 0, false, fmt.Errorf("list activities: %w", aerr)
		}
		if activity.Action != ActivityActionCommented ||
			activity.Comment == nil {
			continue
		}
		if activity.Comment.ID == commentID {
			return activity.Comment.Version, true, nil
		}
	}
	return 0, false, nil
}

// ListComments lists top-level comments on a pull request, filtered by
// opts. Data Center has no comment-listing endpoint, so comments are read
// from the pull request activity feed.
func (g *Gateway) ListComments(
	ctx context.Context,
	prID int64,
	opts bitbucket.ListCommentsOptions,
) iter.Seq2[*bitbucket.ChangeComment, error] {
	return func(yield func(*bitbucket.ChangeComment, error) bool) {
		// CanUpdateOnly filtering needs the current user's name;
		// resolve it once.
		var (
			currentUser     string
			haveCurrentUser bool
		)
		if opts.CanUpdateOnly {
			user, _, err := g.client.CurrentUser(ctx)
			if err != nil {
				yield(nil, fmt.Errorf("get current user: %w", err))
				return
			}
			currentUser = user.Name
			haveCurrentUser = true
		}

		for activity, err := range g.client.ActivityList(
			ctx, g.repoID.restProjectKey(), g.repoID.slug, prID,
		) {
			if err != nil {
				yield(nil, fmt.Errorf("list activities: %w", err))
				return
			}
			if activity.Action != ActivityActionCommented ||
				activity.Comment == nil {
				continue
			}
			comment := activity.Comment

			if haveCurrentUser && comment.Author.Name != currentUser {
				continue
			}

			item := &bitbucket.ChangeComment{
				ID:      comment.ID,
				PRID:    prID,
				Version: comment.Version,
				Body:    comment.Text,
			}
			if !yield(item, nil) {
				return
			}
		}
	}
}

// ResolvableComments lists review comments on a pull request
// that participate in comment resolution counts.
//
// Review-comment threads come from the activity feed; the blocker-comments
// (task) list is then walked to add tasks nested as replies, which the feed
// omits. The two sources are deduplicated by comment ID.
//
// A thread is resolved by its "Resolve" action (ThreadResolved) or, for a
// task, by State "RESOLVED". Unpublished drafts (State "PENDING") are
// reported with Pending set; the caller decides whether to count them.
// The blocker-comments endpoint needs Data Center 7.2+; on older servers
// only the activity-feed comments are emitted.
func (g *Gateway) ResolvableComments(
	ctx context.Context,
	prID int64,
) iter.Seq2[*bitbucket.ResolvableComment, error] {
	return func(yield func(*bitbucket.ResolvableComment, error) bool) {
		seen := make(map[int64]struct{})
		for activity, err := range g.client.ActivityList(
			ctx, g.repoID.restProjectKey(), g.repoID.slug, prID,
		) {
			if err != nil {
				yield(nil, fmt.Errorf("list activities: %w", err))
				return
			}
			if activity.Action != ActivityActionCommented ||
				activity.Comment == nil {
				continue
			}
			c := activity.Comment

			if _, ok := seen[c.ID]; ok {
				continue
			}
			seen[c.ID] = struct{}{}

			item := &bitbucket.ResolvableComment{
				ID:       c.ID,
				Body:     c.Text,
				Resolved: c.ThreadResolved || (c.Severity == "BLOCKER" && c.State == "RESOLVED"),
				Pending:  c.State == "PENDING",
			}
			if !yield(item, nil) {
				return
			}
		}

		// Add tasks nested as replies, which the feed omits.
		for c, err := range g.client.BlockerCommentList(
			ctx, g.repoID.restProjectKey(), g.repoID.slug, prID,
		) {
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					// blocker-comments needs Data Center 7.2+; tolerate its absence.
					return
				}
				yield(nil, fmt.Errorf("list blocker comments: %w", err))
				return
			}

			if _, ok := seen[c.ID]; ok {
				continue
			}
			seen[c.ID] = struct{}{}

			item := &bitbucket.ResolvableComment{
				ID:       c.ID,
				Body:     c.Text,
				Resolved: c.State == "RESOLVED",
				Pending:  c.State == "PENDING",
			}
			if !yield(item, nil) {
				return
			}
		}
	}
}

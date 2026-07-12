package github

import (
	"context"
	"fmt"
	"strings"
)

// PullRequestMetadataInput specifies additive pull request metadata.
type PullRequestMetadataInput struct {
	// PullRequestID identifies the pull request to update.
	PullRequestID ID // required

	// LabelIDs contains labels to add.
	// A nil or empty slice omits the labels mutation; it does not clear labels.
	LabelIDs []ID

	// ReviewerUserIDs contains users from whom to request review.
	// The reviews mutation is omitted when both reviewer slices are empty.
	// Omission does not clear existing review requests.
	ReviewerUserIDs []ID

	// ReviewerTeamIDs contains teams from which to request review.
	// The reviews mutation is omitted when both reviewer slices are empty.
	// Omission does not clear existing review requests.
	ReviewerTeamIDs []ID

	// AssigneeIDs contains users to assign.
	// A nil or empty slice omits the assignees mutation; it does not clear assignees.
	AssigneeIDs []ID
}

// AddPullRequestMetadata adds labels, review requests, and assignees in one
// GraphQL operation.
//
// GitHub executes each requested mutation field independently in textual
// order. If one field fails, successful siblings may still apply and the
// returned error path identifies the failed labels, reviews, or assignees
// alias. AddPullRequestMetadata does not retry.
//
// Base and draft changes are deliberately excluded. Applying later metadata
// after either workflow transition fails would change the forge's existing
// stop-on-error behavior.
//
// See https://docs.github.com/en/graphql/reference/issues#addlabelstolabelable,
// https://docs.github.com/en/graphql/reference/pulls#requestreviews, and
// https://docs.github.com/en/graphql/reference/issues#addassigneestoassignable.
func (c *Gateway) AddPullRequestMetadata(ctx context.Context, input *PullRequestMetadataInput) error {
	// Each requested metadata kind becomes one aliased top-level mutation:
	//
	// mutation($labels:AddLabelsToLabelableInput!) {
	//   labels: addLabelsToLabelable(input: $labels) { clientMutationId }
	// }
	//
	// Multiple kinds share the operation in labels, reviews, assignees order:
	//
	// mutation(
	//   $labels: AddLabelsToLabelableInput!
	//   $reviews: RequestReviewsInput!
	//   $assignees: AddAssigneesToAssignableInput!
	// ) {
	//   labels: addLabelsToLabelable(input: $labels) { clientMutationId }
	//   reviews: requestReviews(input: $reviews) { clientMutationId }
	//   assignees: addAssigneesToAssignable(input: $assignees) { clientMutationId }
	// }
	variables := make(map[string]any, 3)
	var variableDefinitions strings.Builder
	var fields strings.Builder
	addMutation := func(alias, inputType, field string, value any) {
		fmt.Fprintf(&variableDefinitions, "$%s:%s!", alias, inputType)
		if fields.Len() > 0 {
			fields.WriteByte(',')
		}
		fmt.Fprintf(
			&fields,
			"%s:%s(input: $%s){clientMutationId}",
			alias,
			field,
			alias,
		)
		variables[alias] = value
	}

	if len(input.LabelIDs) > 0 {
		addMutation("labels", "AddLabelsToLabelableInput", "addLabelsToLabelable", struct {
			LabelableID ID   `json:"labelableId"`
			LabelIDs    []ID `json:"labelIds"`
		}{input.PullRequestID, input.LabelIDs})
	}
	if len(input.ReviewerUserIDs) > 0 || len(input.ReviewerTeamIDs) > 0 {
		addMutation("reviews", "RequestReviewsInput", "requestReviews", struct {
			PullRequestID ID   `json:"pullRequestId"`
			UserIDs       []ID `json:"userIds,omitempty"`
			TeamIDs       []ID `json:"teamIds,omitempty"`
			Union         bool `json:"union"`
		}{input.PullRequestID, input.ReviewerUserIDs, input.ReviewerTeamIDs, true})
	}
	if len(input.AssigneeIDs) > 0 {
		addMutation("assignees", "AddAssigneesToAssignableInput", "addAssigneesToAssignable", struct {
			AssignableID ID   `json:"assignableId"`
			AssigneeIDs  []ID `json:"assigneeIds"`
		}{input.PullRequestID, input.AssigneeIDs})
	}

	if fields.Len() == 0 {
		return nil
	}
	mutation := compactGraphQL(
		"mutation(" + variableDefinitions.String() + "){" + fields.String() + "}",
	)
	return c.execute(ctx, mutation, variables, &struct{}{})
}

package github

import "context"

// AddAssigneesToAssignable adds assignees to an assignable node.
func (c *Gateway) AddAssigneesToAssignable(ctx context.Context, assignableID ID, assigneeIDs []ID) error {
	mutation := compactGraphQL(`
		mutation($input:AddAssigneesToAssignableInput!){
			addAssigneesToAssignable(input: $input){clientMutationId}
		}
	`)
	return c.mutate(ctx, mutation, struct {
		AssignableID ID   `json:"assignableId"`
		AssigneeIDs  []ID `json:"assigneeIds"`
	}{assignableID, assigneeIDs}, &struct{}{})
}

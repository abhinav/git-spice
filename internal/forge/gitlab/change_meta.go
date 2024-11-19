package gitlab

import (
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// MR uniquely identifies an MR in GitLab repository.
// It's a valid forge.ChangeID.
type MR struct {
	// Number is the merge request number.
	// This will always be set.
	Number int `json:"number"`
}

var _ forge.ChangeID = (*MR)(nil)

func (id *MR) String() string {
	return fmt.Sprintf("!%d", id.Number)
}

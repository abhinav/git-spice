package shamhub

import (
	"errors"
	"fmt"
)

// DraftChangeRequest is a request to mark a change as draft.
type DraftChangeRequest struct {
	Owner, Repo string
	Number      int
}

// DraftChange marks an open change as a draft.
func (sh *ShamHub) DraftChange(req DraftChangeRequest) error {
	if req.Owner == "" || req.Repo == "" || req.Number == 0 {
		return errors.New("owner, repo, and number are required")
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	for i, c := range sh.changes {
		if c.Base.Owner == req.Owner && c.Base.Repo == req.Repo && c.Number == req.Number {
			if c.State != shamChangeOpen {
				return fmt.Errorf("change %d is not open", req.Number)
			}
			sh.changes[i].Draft = true
			return nil
		}
	}

	return fmt.Errorf("change %d not found", req.Number)
}

// ReviewChangeRequest is a request to set the review decision on a change.
type ReviewChangeRequest struct {
	Owner, Repo string
	Number      int

	// Decision is the review decision to apply.
	// It must be "review_requested", "changes_requested", or "approved".
	Decision string

	// Reviewer is the username of the reviewer.
	Reviewer string
}

// ReviewChange sets the review decision on an open change.
func (sh *ShamHub) ReviewChange(req ReviewChangeRequest) error {
	if req.Owner == "" || req.Repo == "" || req.Number == 0 {
		return errors.New("owner, repo, and number are required")
	}

	var decision shamReviewDecision
	switch req.Decision {
	case "review_requested":
		decision = shamReviewRequired
	case "changes_requested":
		decision = shamReviewChangesRequested
	case "approved":
		decision = shamReviewApproved
	default:
		return fmt.Errorf("unknown review decision %q", req.Decision)
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	for i, c := range sh.changes {
		if c.Base.Owner == req.Owner && c.Base.Repo == req.Repo && c.Number == req.Number {
			if c.State != shamChangeOpen {
				return fmt.Errorf("change %d is not open", req.Number)
			}

			sh.changes[i].ReviewDecision = decision
			if req.Reviewer != "" && decision == shamReviewRequired {
				sh.changes[i].RequestedReviewers = append(
					sh.changes[i].RequestedReviewers,
					req.Reviewer,
				)
			}
			return nil
		}
	}

	return fmt.Errorf("change %d not found", req.Number)
}

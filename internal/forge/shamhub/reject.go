package shamhub

import (
	"errors"
	"fmt"
)

// RejectChangeRequest is a request to reject a change.
type RejectChangeRequest struct {
	Owner, Repo string
	Number      int
}

// RejectChange closes a CR without merging it.
func (sh *ShamHub) RejectChange(req RejectChangeRequest) error {
	if req.Owner == "" || req.Repo == "" || req.Number == 0 {
		return errors.New("owner, repo, and number are required")
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()

	var changeIdx int
	for idx, change := range sh.changes {
		if change.Owner == req.Owner && change.Repo == req.Repo && change.Number == req.Number {
			changeIdx = idx
			break
		}
	}

	if sh.changes[changeIdx].State != shamChangeOpen {
		return fmt.Errorf("change %d is not open", req.Number)
	}

	sh.changes[changeIdx].State = shamChangeClosed
	return nil
}

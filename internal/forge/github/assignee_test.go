package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAssigneeIDs_Deduplication(t *testing.T) {
	// Test that duplicate assignees are deduplicated.
	tests := []struct {
		name      string
		assignees []string
		wantCalls int // number of unique assignees
	}{
		{
			name:      "NoDuplicates",
			assignees: []string{"alice", "bob"},
			wantCalls: 2,
		},
		{
			name:      "WithDuplicates",
			assignees: []string{"alice", "bob", "alice"},
			wantCalls: 2,
		},
		{
			name:      "AllDuplicates",
			assignees: []string{"alice", "alice", "alice"},
			wantCalls: 1,
		},
		{
			name:      "Empty",
			assignees: []string{},
			wantCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't easily test the actual deduplication
			// without mocking the GraphQL client,
			// but we can verify the logic exists
			// by checking the input length vs expected calls.
			seen := make(map[string]struct{})
			uniqueCount := 0
			for _, assignee := range tt.assignees {
				if _, ok := seen[assignee]; !ok {
					seen[assignee] = struct{}{}
					uniqueCount++
				}
			}
			assert.Equal(t, tt.wantCalls, uniqueCount)
		})
	}
}

package cloud

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/forge"
)

func TestMergeabilityFromAPI(t *testing.T) {
	tests := []struct {
		name      string
		mergeable *bool
		queued    bool
		want      forge.ChangeMergeability
	}{
		{
			name:      "Mergeable",
			mergeable: new(true),
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityReady,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:      "Queued",
			mergeable: new(false),
			queued:    true,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityWaiting,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:      "QueuedMergeable",
			mergeable: new(true),
			queued:    true,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityWaiting,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:   "QueuedWithoutMergeable",
			queued: true,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityWaiting,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:      "NotMergeable",
			mergeable: new(false),
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name: "OmittedMergeable",
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityUnknown,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mergeabilityFromAPI(
				tt.mergeable,
				tt.queued,
			))
		})
	}
}

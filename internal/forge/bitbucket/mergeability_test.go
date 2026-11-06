package bitbucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
	"go.uber.org/mock/gomock"
)

func TestChangeMergeability(t *testing.T) {
	tests := []struct {
		name         string
		mergeability forge.ChangeMergeability
	}{
		{
			name: "Mergeable",
			mergeability: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityReady,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name: "Queued",
			mergeability: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityWaiting,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name: "NotMergeable",
			mergeability: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name: "OmittedMergeable",
			mergeability: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityUnknown,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)

			mockGateway := NewMockGateway(mockCtrl)
			mockGateway.EXPECT().
				ChangeMergeability(gomock.Any(), int64(1)).
				Return(tt.mergeability, nil)

			repo := newRepository(new(Forge), silog.Nop(), mockGateway)
			got, err := repo.ChangeMergeability(t.Context(), &PR{Number: 1})
			require.NoError(t, err)
			assert.Equal(t, tt.mergeability, got)
		})
	}
}

package submit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/git"
)

func TestClassifyRemote(t *testing.T) {
	const (
		lastPushed = git.Hash("1111111111111111111111111111111111111111")
		remoteHead = git.Hash("2222222222222222222222222222222222222222")
		local      = git.Hash("3333333333333333333333333333333333333333")
	)

	tests := []struct {
		name string

		lastPushed git.Hash
		remoteHead git.Hash
		local      git.Hash

		// ffAncestor is the result of isAncestor(remoteHead, local):
		// whether the push would be a fast-forward.
		ffAncestor bool

		// advAncestor is the result of isAncestor(lastPushed, remoteHead):
		// whether the remote advanced from our last push.
		advAncestor bool

		want remoteDecision
	}{
		{
			name:       "RemoteAbsent",
			lastPushed: lastPushed,
			remoteHead: git.ZeroHash,
			local:      local,
			want:       decisionSafe,
		},
		{
			name:       "UpToDate",
			lastPushed: lastPushed,
			remoteHead: local,
			local:      local,
			want:       decisionUpToDate,
		},
		{
			name:       "FastForward",
			lastPushed: lastPushed,
			remoteHead: remoteHead,
			local:      local,
			ffAncestor: true,
			want:       decisionFastForward,
		},
		{
			name:       "NoBaseline",
			lastPushed: git.ZeroHash,
			remoteHead: remoteHead,
			local:      local,
			want:       decisionNoBaseline,
		},
		{
			name:       "RemoteUnchangedAfterRestack",
			lastPushed: lastPushed,
			remoteHead: lastPushed,
			local:      local,
			want:       decisionSafe,
		},
		{
			name:        "Advanced",
			lastPushed:  lastPushed,
			remoteHead:  remoteHead,
			local:       local,
			advAncestor: true,
			want:        decisionAdvanced,
		},
		{
			name:       "Diverged",
			lastPushed: lastPushed,
			remoteHead: remoteHead,
			local:      local,
			want:       decisionDiverged,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyRemote(
				tt.lastPushed, tt.remoteHead, tt.local,
				func(a, b git.Hash) bool {
					switch {
					case a == tt.remoteHead && b == tt.local:
						return tt.ffAncestor
					case a == tt.lastPushed && b == tt.remoteHead:
						return tt.advAncestor
					default:
						t.Fatalf("unexpected isAncestor(%v, %v)", a, b)
						return false
					}
				},
			)
			assert.Equal(t, tt.want, got)
		})
	}
}

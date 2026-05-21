package scriptrun_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/scriptrun"
)

func TestEnvFor(t *testing.T) {
	tests := []struct {
		name   string
		op     scriptrun.Operation
		branch string
		base   string
		want   []string
	}{
		{
			name: "OperationOnly",
			op:   scriptrun.OpIntegrationRebuild,
			want: []string{"GS_OPERATION=integration-rebuild"},
		},
		{
			name:   "BranchPresent",
			op:     scriptrun.OpBranchRestack,
			branch: "feat1",
			want: []string{
				"GS_OPERATION=branch-restack",
				"GS_BRANCH=feat1",
			},
		},
		{
			name:   "AllThree",
			op:     scriptrun.OpCommitCreate,
			branch: "feat1",
			base:   "main",
			want: []string{
				"GS_OPERATION=commit-create",
				"GS_BRANCH=feat1",
				"GS_BASE=main",
			},
		},
		{
			name: "BaseWithoutBranchOmitsBoth",
			op:   scriptrun.OpStackRestack,
			base: "main",
			want: []string{
				"GS_OPERATION=stack-restack",
				"GS_BASE=main",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scriptrun.EnvFor(tt.op, tt.branch, tt.base)
			assert.Equal(t, tt.want, got)
		})
	}
}

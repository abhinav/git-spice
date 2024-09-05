package sliceutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/sliceutil"
)

func TestCollectErr(t *testing.T) {
	type pair struct {
		val int
		err error
	}

	tests := []struct {
		name string
		give []pair

		want    []int
		wantErr error
	}{
		{
			name: "Empty",
			give: nil,
			want: nil,
		},
		{
			name: "NoErrors",
			give: []pair{
				{val: 1},
				{val: 2},
				{val: 3},
			},
			want: []int{1, 2, 3},
		},
		{
			name: "Error",
			give: []pair{
				{val: 1},
				{err: assert.AnError},
				{val: 3},
			},
			wantErr: assert.AnError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sliceutil.CollectErr(func(yield func(int, error) bool) {
				for _, p := range tt.give {
					if !yield(p.val, p.err) {
						break
					}
				}
			})

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

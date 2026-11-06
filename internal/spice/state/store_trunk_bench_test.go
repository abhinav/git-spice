package state

import (
	"fmt"
	"testing"
)

func BenchmarkStoreTrunkTransfer(b *testing.B) {
	for _, size := range []int{1, 100} {
		b.Run(fmt.Sprintf("Branches%d", size), func(b *testing.B) {
			_, db := benchmarkGitStore(b, size)
			from, to := "main", "other"
			for b.Loop() {
				if err := transferTrunkBranch(b.Context(), db, from, to); err != nil {
					b.Fatal(err)
				}
				from, to = to, from
			}
		})
	}
}

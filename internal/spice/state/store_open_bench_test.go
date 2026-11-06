package state

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func BenchmarkStoreOpen(b *testing.B) {
	for _, size := range []int{1, 100} {
		for _, legacy := range []bool{false, true} {
			b.Run(fmt.Sprintf("Branches%d/Legacy%t", size, legacy), func(b *testing.B) {
				_, db := benchmarkGitStore(b, size)
				if legacy {
					require.NoError(b, db.Delete(b.Context(), _versionFile, "legacy layout"))
				}
				for b.Loop() {
					if _, err := OpenStore(b.Context(), db, silogtest.New(b)); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

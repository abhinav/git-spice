package state

import (
	"fmt"
	"testing"
)

func BenchmarkBranchTxDelete(b *testing.B) {
	for _, size := range []int{1, 100} {
		b.Run(fmt.Sprintf("Branches%d", size), func(b *testing.B) {
			store, _ := benchmarkGitStore(b, size)
			for b.Loop() {
				tx := store.BeginBranchTx()
				if err := tx.Delete(b.Context(), "branch-000"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkBranchTxDeleteMany(b *testing.B) {
	for _, size := range []int{1, 100} {
		b.Run(fmt.Sprintf("Branches%d", size), func(b *testing.B) {
			store, _ := benchmarkGitStore(b, size)
			names := make([]string, size)
			for i := range names {
				names[i] = fmt.Sprintf("branch-%03d", i)
			}
			for b.Loop() {
				tx := store.BeginBranchTx()
				if err := tx.DeleteMany(b.Context(), names); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

package state

import (
	"fmt"
	"testing"
)

func BenchmarkBranchTxUpdate(b *testing.B) {
	for _, size := range []int{1, 100} {
		b.Run(fmt.Sprintf("Branches%d", size), func(b *testing.B) {
			store, _ := benchmarkGitStore(b, size)
			reqs := make([]UpsertRequest, size)
			for i := range reqs {
				reqs[i] = UpsertRequest{Name: fmt.Sprintf("branch-%03d", i), BaseHash: "updated"}
			}
			for b.Loop() {
				tx := store.BeginBranchTx()
				if err := tx.UpsertMany(b.Context(), reqs); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkBranchTxTrack(b *testing.B) {
	for _, size := range []int{1, 100} {
		b.Run(fmt.Sprintf("Branches%d", size), func(b *testing.B) {
			store, _ := benchmarkGitStore(b, size)
			reqs := make([]UpsertRequest, size)
			base := "main"
			for i := range reqs {
				name := fmt.Sprintf("new-%03d", i)
				reqs[i] = UpsertRequest{Name: name, Base: base}
				base = name
			}
			for b.Loop() {
				tx := store.BeginBranchTx()
				if err := tx.UpsertMany(b.Context(), reqs); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

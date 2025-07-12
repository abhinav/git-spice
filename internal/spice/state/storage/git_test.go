package storage

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestGitBackendUpdateNoChanges(t *testing.T) {
	ctx := t.Context()
	repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	backend := NewGitBackend(GitConfig{
		Repo:        repo,
		Ref:         "refs/data",
		AuthorName:  "Test Author",
		AuthorEmail: "test@example.com",
		Log:         silogtest.New(t),
	})

	db := NewDB(backend)
	require.NoError(t, db.Set(ctx, "foo", "bar", "initial set"))

	start, err := repo.PeelToCommit(ctx, "refs/data")
	require.NoError(t, err)

	require.NoError(t, db.Set(ctx, "foo", "bar", "shrug"))

	end, err := repo.PeelToCommit(ctx, "refs/data")
	require.NoError(t, err)

	assert.Equal(t, start, end,
		"there should be no changes in the repository")
}

func TestGitBackend_ConcurrentOperations(t *testing.T) {
	var seed [32]byte
	if seedstr := os.Getenv("GIT_BACKEND_CONCURRENT_SEED"); seedstr != "" {
		bs, err := hex.DecodeString(seedstr)
		require.NoError(t, err)
		require.Len(t, bs, 32)
		copy(seed[:], bs)
	} else {
		_, _ = cryptorand.Read(seed[:]) // cannot fail
		t.Logf("GIT_BACKEND_CONCURRENT_SEED=%s", hex.EncodeToString(seed[:]))
	}
	testRand := rand.New(rand.NewChaCha8(seed))

	ctx := t.Context()
	repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: silog.Nop(),
	})
	require.NoError(t, err)

	backend := NewGitBackend(GitConfig{
		Repo:        repo,
		Ref:         "refs/data",
		AuthorName:  "Test Author",
		AuthorEmail: "test@example.com",
		Log:         silogtest.New(t),
	})

	const (
		NumWorkers   = 10
		OpsPerWorker = 10
		NumKeys      = 1000
	)

	db := NewDB(backend)

	// Pre-populate some data
	require.NoError(t, db.Set(ctx, "initial/key1", "value1", "initial data"))
	require.NoError(t, db.Set(ctx, "initial/key2", "value2", "initial data"))

	ops := []struct {
		name   string
		op     func(*rand.Rand)
		weight float64 // weight for random selection
	}{
		{
			name:   "Keys",
			weight: 1,
			op: func(*rand.Rand) {
				_, err := backend.Keys(ctx, "")
				assert.NoError(t, err, "Keys operation failed")
			},
		},
		{
			name:   "Get",
			weight: 1,
			op: func(*rand.Rand) {
				var value string
				err := backend.Get(ctx, "initial/key1", &value)
				if !errors.Is(err, ErrNotExist) {
					assert.NoError(t, err, "Get operation failed")
				}
			},
		},
		{
			name:   "Update",
			weight: 1,
			op: func(rand *rand.Rand) {
				key := fmt.Sprintf("key%d", rand.IntN(NumKeys))
				value := rand.Int()
				err := backend.Update(ctx, UpdateRequest{
					Sets: []SetRequest{{
						Key:   key,
						Value: value,
					}},
					Message: "concurrent update",
				})
				assert.NoError(t, err, "Update operation failed")
			},
		},
		{
			name:   "Clear",
			weight: 0.3, // Less frequent
			op: func(*rand.Rand) {
				err := backend.Clear(ctx, "concurrent clear operation")
				assert.NoError(t, err, "Clear operation failed")
			},
		},
	}

	var totalWeight float64
	for _, op := range ops {
		totalWeight += op.weight
	}

	selectOp := func(rand *rand.Rand) (name string, f func(*rand.Rand)) {
		// Generate a random value in [0, totalWeight)
		// and then search through the operations
		// to find where it falls.
		target := rand.Float64() * totalWeight // [0, totalWeight)

		var cursor float64
		for _, op := range ops {
			cursor += op.weight
			if cursor >= target {
				return op.name, op.op
			}
		}

		panic("should never reach here, totalWeight is not zero")
	}

	var wg sync.WaitGroup
	for workerIdx := range NumWorkers {
		wg.Add(1)

		seed1, seed2 := testRand.Uint64(), testRand.Uint64()
		go func() {
			defer wg.Done()

			rand := rand.New(rand.NewPCG(seed1, seed2))
			for opIdx := range OpsPerWorker {
				name, op := selectOp(rand)
				t.Logf("[worker %d, op %d] %s", workerIdx, opIdx, name)
				op(rand)
			}
		}()
	}

	wg.Wait()

	// Verify the backend is still functional after concurrent operations
	err = db.Set(ctx, "post_test", "final_value", "post-test verification")
	require.NoError(t, err)

	var finalValue string
	err = backend.Get(ctx, "post_test", &finalValue)
	require.NoError(t, err)
	assert.Equal(t, "final_value", finalValue)
}

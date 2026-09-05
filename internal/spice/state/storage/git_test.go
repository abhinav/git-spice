package storage

import (
	"context"
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

func TestGitBackendUpdateNoChanges_concurrentUpdate(t *testing.T) {
	ctx := t.Context()
	repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	newBackend := func(repo GitRepository) *GitBackend {
		return NewGitBackend(GitConfig{
			Repo:        repo,
			Ref:         "refs/data",
			AuthorName:  "Test Author",
			AuthorEmail: "test@example.com",
			Log:         silogtest.New(t),
		})
	}
	concurrent := NewDB(newBackend(repo))
	require.NoError(t, concurrent.Set(ctx, "foo", "bar", "initial set"))

	racingRepo := &updateTreeHookRepository{
		GitRepository: repo,
		BeforeUpdate: func(ctx context.Context, call int) error {
			return concurrent.Set(
				ctx,
				fmt.Sprintf("concurrent/%d", call),
				call,
				"concurrent update",
			)
		},
	}
	db := NewDB(newBackend(racingRepo))

	require.NoError(t, db.Set(ctx, "foo", "bar", "keep desired value"))
	assert.Equal(t, 1, racingRepo.calls)
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

func TestGitBackend_SpecialCharacterKeys(t *testing.T) {
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

	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"Greek theta", "branches/feature/θ-theta", "theta-value"},
		{"Unicode checkmark", "branches/feature/✅-checkmark", "checkmark-value"},
		{"Combined emoji", "branches/feature/👨‍💻-developer", "developer-value"},
		{"Accented character", "branches/feature/café", "cafe-value"},
		{"Japanese characters", "branches/feature/日本語", "japanese-value"},
	}

	db := NewDB(backend)

	// Store all values
	for _, tt := range tests {
		t.Run("Set/"+tt.name, func(t *testing.T) {
			err := db.Set(ctx, tt.key, tt.value, "add "+tt.name)
			require.NoError(t, err)
		})
	}

	// Retrieve all values
	for _, tt := range tests {
		t.Run("Get/"+tt.name, func(t *testing.T) {
			var got string
			err := backend.Get(ctx, tt.key, &got)
			require.NoError(t, err)
			assert.Equal(t, tt.value, got)
		})
	}

	// List keys and verify all special character keys are present
	t.Run("Keys", func(t *testing.T) {
		keys, err := backend.Keys(ctx, "branches")
		require.NoError(t, err)

		expectedKeys := []string{
			"feature/θ-theta",
			"feature/✅-checkmark",
			"feature/👨‍💻-developer",
			"feature/café",
			"feature/日本語",
		}

		for _, expectedKey := range expectedKeys {
			assert.Contains(t, keys, expectedKey,
				"Keys list should contain %q", expectedKey)
		}
	})
}

type updateTreeHookRepository struct {
	GitRepository
	BeforeUpdate func(context.Context, int) error
	calls        int
}

func (r *updateTreeHookRepository) UpdateTree(
	ctx context.Context,
	req git.UpdateTreeRequest,
) (git.Hash, error) {
	r.calls++
	if err := r.BeforeUpdate(ctx, r.calls); err != nil {
		return "", err
	}
	return r.GitRepository.UpdateTree(ctx, req)
}

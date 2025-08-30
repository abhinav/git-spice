package fixturetest_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/fixturetest"
)

func TestFixture(t *testing.T) {
	t.Chdir(t.TempDir())

	var giveUpdate bool

	cfg := fixturetest.Config{Update: func() bool { return giveUpdate }}
	fixture := fixturetest.New(cfg, "number", rand.Int)

	// Initial generation.
	giveUpdate = true
	v1 := fixture.Get(t)

	// Read from disk.
	giveUpdate = false
	assert.Equal(t, v1, fixture.Get(t))

	// Update again.
	giveUpdate = true
	// At least one attempt out of N should succeed.
	assert.EventuallyWithT(t, func(t *assert.CollectT) {
		v2 := fixture.Get(collectTAdapter{t, "Update"})
		assert.NotEqual(t, v1, v2)
	}, time.Second, 10*time.Millisecond)
}

type collectTAdapter struct {
	*assert.CollectT

	name string
}

var _ fixturetest.TestingT = collectTAdapter{}

func (collectTAdapter) Helper() {}

func (c collectTAdapter) Name() string {
	return c.name
}

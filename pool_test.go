package van

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPool_Get(t *testing.T) {
	pool := newPool()
	_, sla := pool.get(5)
	_, slb := pool.get(5)

	assert.Equal(t, int32(2), pool.allocated)
	assert.Equal(t, int32(2), pool.inuse)

	assert.Len(t, sla, 5)
	assert.Len(t, slb, 5)
}

func TestPool_GetLargerItem(t *testing.T) {
	pool := newPool()
	size := poolItemSize + 1
	buf, sl := pool.get(size)

	assert.Equal(t, int32(0), pool.allocated)
	assert.Equal(t, int32(0), pool.inuse)
	assert.Nil(t, buf)
	assert.Len(t, sl, size)
}

func TestPool_GetEmptyPool(t *testing.T) {
	pool := newPool()
	for i := 0; i < poolSize; i++ {
		pool.get(0)
	}
	require.Equal(t, int32(poolSize), pool.allocated)
	require.Equal(t, int32(poolSize), pool.inuse)

	buf, sl := pool.get(poolItemSize)
	assert.Len(t, sl, poolItemSize)
	assert.NotNil(t, buf)
	assert.Equal(t, int32(poolSize+1), pool.allocated)
	assert.Equal(t, int32(poolSize+1), pool.inuse)

}

func TestPool_Put(t *testing.T) {
	pool := newPool()
	b1, _ := pool.get(poolItemSize)

	pool.get(poolItemSize)
	assert.Equal(t, int32(2), pool.allocated)
	assert.Equal(t, int32(2), pool.inuse)

	pool.put(b1)
	assert.Equal(t, int32(2), pool.allocated)
	assert.Equal(t, int32(1), pool.inuse)
}

func TestPool_GetPutReuse(t *testing.T) {
	pool := newPool()
	ba, _ := pool.get(poolItemSize)
	pool.put(ba)

	bb, _ := pool.get(poolItemSize)
	assert.Equal(t, ba, bb)
}

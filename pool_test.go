package van

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestArgsPool_Get(t *testing.T) {
	pool := newPool(5)
	_, sliceA := pool.get(5)
	_, sliceB := pool.get(5)
	assert.Equal(t, int32(2), pool.allocated)
	assert.Equal(t, int32(2), pool.inuse)
	assert.Len(t, sliceA, 5)
	assert.Len(t, sliceB, 5)
}

func TestArgsPool_Reuse(t *testing.T) {
	pool := newPool(5)
	bufA, _ := pool.get(5)
	pool.put(bufA)
	bufB, _ := pool.get(5)
	assert.Equal(t, int32(1), pool.allocated)
	assert.Equal(t, int32(1), pool.inuse)
	assert.Equal(t, bufA, bufB)
}

func TestArgsPool_Larger(t *testing.T) {
	pool := newPool(1)
	_, sl := pool.get(2)

	assert.Equal(t, int32(0), pool.allocated)
	assert.Equal(t, int32(0), pool.inuse)
	assert.Len(t, sl, 2)
}

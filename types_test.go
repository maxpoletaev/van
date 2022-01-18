package van

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsStructPtr(t *testing.T) {
	assert.True(t, isStructPtr(reflect.TypeOf(&struct{}{})))
	assert.False(t, isStructPtr(reflect.TypeOf(struct{}{})))
	assert.False(t, isStructPtr(reflect.TypeOf(1)))
}

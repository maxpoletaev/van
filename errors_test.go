package van

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestError_Unwrap(t *testing.T) {
	baseErr := newError("base error")
	err := baseErr.new("new error")
	assert.True(t, errors.Is(err, baseErr))
}

func TestError_Fmt(t *testing.T) {
	err := newError("base error").fmt("new error %d", 1)
	assert.Equal(t, "new error 1", err.Error())
}

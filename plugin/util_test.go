package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasTail(t *testing.T) {
	assert.Equal(t, true, hasBase("example.com/hello/world", "example.com/hello/world"))
	assert.Equal(t, true, hasBase("example.com/hello/world", "hello/world"))
	assert.Equal(t, true, hasBase("example.com/hello/world", "world"))
	assert.Equal(t, false, hasBase("example.com/hello/world", "orld"))
	assert.Equal(t, false, hasBase("example.com/hello/world", "/world"))
}

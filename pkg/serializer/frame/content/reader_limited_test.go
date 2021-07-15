package content

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_OrDefaultMaxFrameSize(t *testing.T) {
	assert.Equal(t, OrDefaultMaxFrameSize(1234), int64(1234))
	assert.Equal(t, OrDefaultMaxFrameSize(0), int64(DefaultMaxFrameSize))
}

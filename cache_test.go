package cache_lib_go_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	cache_lib_go "github.com/honestbank/cache-lib-go"
)

func TestCache(t *testing.T) {
	a := assert.New(t)
	err := cache_lib_go.Cache()

	a.NoError(err)
}

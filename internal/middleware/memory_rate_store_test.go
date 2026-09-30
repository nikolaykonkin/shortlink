package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMemoryRateStore_Allow проверяет хранилище напрямую, без RateLimiter:
// лимит по ключу, независимость ключей и то, что ошибка всегда nil
func TestMemoryRateStore_Allow(t *testing.T) {
	store := NewMemoryRateStore()
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		allowed, err := store.Allow(ctx, "a", 2, time.Hour)
		require.NoError(t, err)
		assert.True(t, allowed, "запрос %d по ключу a укладывается в лимит", i)
	}

	allowed, err := store.Allow(ctx, "a", 2, time.Hour)
	require.NoError(t, err)
	assert.False(t, allowed, "третий запрос по ключу a превышает лимит")

	allowed, err = store.Allow(ctx, "b", 2, time.Hour)
	require.NoError(t, err)
	assert.True(t, allowed, "счетчики разных ключей независимы")
}

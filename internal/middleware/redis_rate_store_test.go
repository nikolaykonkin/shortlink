package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRedisStore поднимает miniredis — Redis в памяти процесса теста, без Docker —
// и возвращает хранилище поверх него
// miniredis останавливается автоматически по окончании теста
// Время в нем идет только вручную: TTL уменьшается через FastForward, поэтому тесты не ждут по-настоящему
func newTestRedisStore(t *testing.T) (*RedisRateStore, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	return NewRedisRateStore(client), mr
}

func TestRedisRateStore_Allow_Limit(t *testing.T) {
	store, _ := newTestRedisStore(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		allowed, err := store.Allow(ctx, "user:1", 3, time.Minute)
		require.NoError(t, err)
		assert.True(t, allowed, "запрос %d укладывается в лимит", i)
	}

	allowed, err := store.Allow(ctx, "user:1", 3, time.Minute)
	require.NoError(t, err)
	assert.False(t, allowed, "четвертый запрос превышает лимит")
}

func TestRedisRateStore_Allow_KeysAreIndependent(t *testing.T) {
	store, _ := newTestRedisStore(t)
	ctx := context.Background()

	allowed, err := store.Allow(ctx, "user:1", 1, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = store.Allow(ctx, "user:1", 1, time.Minute)
	require.NoError(t, err)
	require.False(t, allowed)

	allowed, err = store.Allow(ctx, "user:2", 1, time.Minute)
	require.NoError(t, err)
	assert.True(t, allowed, "счетчик другого ключа не затронут")
}

// TestRedisRateStore_Allow_StoresPrefixedKeyWithTTL проверяет то, что видно в самом Redis:
// ключ с префиксом и TTL, равный окну - без TTL счетчик жил бы вечно
func TestRedisRateStore_Allow_StoresPrefixedKeyWithTTL(t *testing.T) {
	store, mr := newTestRedisStore(t)

	allowed, err := store.Allow(context.Background(), "user:1", 5, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)

	assert.Equal(t, []string{"ratelimit:user:1"}, mr.Keys())
	assert.Equal(t, time.Minute, mr.TTL("ratelimit:user:1"))
}

func TestRedisRateStore_Allow_ResetsAfterTTL(t *testing.T) {
	store, mr := newTestRedisStore(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		allowed, err := store.Allow(ctx, "user:1", 2, time.Minute)
		require.NoError(t, err)
		require.True(t, allowed)
	}

	allowed, err := store.Allow(ctx, "user:1", 2, time.Minute)
	require.NoError(t, err)
	require.False(t, allowed)

	// перематываем время за границу окна — ключ с истекшим TTL исчезает
	mr.FastForward(time.Minute + time.Second)

	allowed, err = store.Allow(ctx, "user:1", 2, time.Minute)
	require.NoError(t, err)
	assert.True(t, allowed, "в новом окне лимит снова доступен")
}

// TestRedisRateStore_Allow_RejectedRequestDoesNotExtendWindow фиксирует семантику окна:
// оно начинается с первого запроса и не продлевается ни разрешенными, ни отклоненными запросами
// Если бы TTL обновлялся на каждый запрос, ключ дожил бы до третьего вызова и тот был бы отклонен
func TestRedisRateStore_Allow_RejectedRequestDoesNotExtendWindow(t *testing.T) {
	store, mr := newTestRedisStore(t)
	ctx := context.Background()

	allowed, err := store.Allow(ctx, "user:1", 1, 10*time.Second)
	require.NoError(t, err)
	require.True(t, allowed)

	mr.FastForward(6 * time.Second)

	allowed, err = store.Allow(ctx, "user:1", 1, 10*time.Second)
	require.NoError(t, err)
	require.False(t, allowed, "лимит исчерпан, окно еще идет")

	// с первого запроса прошло 11 секунд: окно закончилось, хотя отклоненный запрос был 5 секунд назад
	mr.FastForward(5 * time.Second)

	allowed, err = store.Allow(ctx, "user:1", 1, 10*time.Second)
	require.NoError(t, err)
	assert.True(t, allowed, "окно отсчитывается от первого запроса")
}

// TestRedisRateStore_Allow_FailsClosedWhenRedisIsDown — главная гарантия распределенного режима:
// при недоступном Redis хранилище возвращает ошибку и не разрешает запрос
func TestRedisRateStore_Allow_FailsClosedWhenRedisIsDown(t *testing.T) {
	store, mr := newTestRedisStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	allowed, err := store.Allow(ctx, "user:1", 1, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)

	// Redis "падает" уже после того, как клиент успел с ним поработать
	mr.Close()

	allowed, err = store.Allow(ctx, "user:1", 1, time.Minute)

	require.Error(t, err)
	assert.False(t, allowed, "при ошибке запрос нельзя считать разрешенным")
}

func TestRedisRateStore_Allow_RejectsTooShortWindow(t *testing.T) {
	store, mr := newTestRedisStore(t)

	allowed, err := store.Allow(context.Background(), "user:1", 1, 0)

	require.Error(t, err)
	assert.False(t, allowed)
	assert.Empty(t, mr.Keys(), "некорректный вызов не должен доходить до Redis")
}

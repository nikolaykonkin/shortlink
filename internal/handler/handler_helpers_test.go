package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nikolaykonkin/shortlink/pkg/auth"
)

// testJWTSecret и testJWTTTL используются во всех тестах хендлеров, где нужна аутентификация —
// значения одинаковые что для AuthMiddleware, что для генерации токена, иначе подпись не сойдется
const (
	testJWTSecret = "test-jwt-secret"
	testJWTTTL    = time.Hour
)

// newTestToken выдает настоящий JWT для userID — тесты проходят его через AuthMiddleware,
// а не кладут userID в контекст напрямую, чтобы проверять реальную связку middleware+хендлер
func newTestToken(t *testing.T, userID int64) string {
	t.Helper()

	token, _, err := auth.GenerateToken(userID, []byte(testJWTSecret), testJWTTTL)
	require.NoError(t, err)

	return token
}

// decodeJSON декодирует тело ответа в T, немедленно завершая тест при ошибке —
// используется только после проверки статус-кода, поэтому невалидное тело здесь уже баг
func decodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var value T
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&value))

	return value
}

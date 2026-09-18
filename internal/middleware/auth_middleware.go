package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/nikolaykonkin/shortlink/pkg/auth"
)

type contextKey string

const userIDContextKey contextKey = "userID"

// AuthMiddleware проверяет JWT из заголовка Authorization и кладет ID пользователя в контекст
func AuthMiddleware(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				http.Error(w, "отсутствует токен авторизации", http.StatusUnauthorized)
				return
			}

			userID, err := auth.ParseToken(token, secret)
			if err != nil {
				// ошибки токена приходят из pkg/auth, а не из apperrors —
				// маппим их в статус прямо здесь, не через apperrors.ToHTTPStatus
				message := "невалидный токен"
				if errors.Is(err, auth.ErrExpiredToken) {
					message = "токен истек"
				}
				http.Error(w, message, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), userIDContextKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext достает ID аутентифицированного пользователя из контекста запроса
func UserIDFromContext(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(userIDContextKey).(int64)
	return userID, ok
}

// bearerToken извлекает значение токена из заголовка вида "Authorization: Bearer <токен>"
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "

	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}

	return strings.TrimPrefix(header, prefix), true
}

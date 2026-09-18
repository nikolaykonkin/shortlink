package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken и ErrExpiredToken объявлены здесь, а не в internal/apperrors —
// pkg/ не должен зависеть от internal/, иначе теряется смысл разделения
var (
	ErrInvalidToken = errors.New("невалидный токен")
	ErrExpiredToken = errors.New("токен истек")
)

// Claims — кастомные claims, несут только ID пользователя поверх стандартных полей JWT
type Claims struct {
	UserID int64 `json:"user_id"`
	jwt.RegisteredClaims
}

// GenerateToken подписывает токен HMAC-SHA256 указанным секретом со сроком действия ttl
// Возвращает подписанный токен и момент истечения — то же значение, что попадает в claims,
// чтобы вызывающий код не вычислял expiresAt повторно
func GenerateToken(userID int64, secret []byte, ttl time.Duration) (string, time.Time, error) {
	expiresAt := time.Now().Add(ttl)

	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("подпись токена: %w", err)
	}

	return signed, expiresAt, nil
}

// ParseToken проверяет подпись и срок действия, возвращает ID пользователя из claims
func ParseToken(tokenString string, secret []byte) (int64, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		// явно требуем HMAC вместо того, чтобы доверять alg из заголовка токена — иначе токен,
		// подписанный алгоритмом none или RSA с публичным ключом вместо секрета, мог бы пройти проверку
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("неожиданный метод подписи: %v", t.Header["alg"])
		}
		return secret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return 0, ErrExpiredToken
		}
		return 0, ErrInvalidToken
	}
	if !token.Valid {
		return 0, ErrInvalidToken
	}

	return claims.UserID, nil
}

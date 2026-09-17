package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword создает хеш пароля используя bcrypt
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("хеширование пароля: %w", err)
	}

	return string(hash), nil
}

// ComparePassword возвращает nil, если password соответствует хешу hash
func ComparePassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

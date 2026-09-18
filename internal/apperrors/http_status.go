package apperrors

import (
	"errors"
	"net/http"
)

// ToHTTPStatus маппит доменную ошибку в HTTP-статус
func ToHTTPStatus(err error) int {
	switch {
	case errors.Is(err, ErrUserNotFound), errors.Is(err, ErrLinkNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrDuplicateUser), errors.Is(err, ErrDuplicateShortCode):
		return http.StatusConflict
	case errors.Is(err, ErrInvalidCredentials):
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

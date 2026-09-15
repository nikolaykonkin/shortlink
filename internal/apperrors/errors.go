package apperrors

import "errors"

var (
	ErrUserNotFound = errors.New("пользователь не найден")

	ErrDuplicateUser = errors.New("пользователь с таким email уже существует")

	ErrLinkNotFound = errors.New("ссылка не найдена")

	ErrDuplicateShortCode = errors.New("такой короткий код уже занят")
)

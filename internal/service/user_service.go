package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nikolaykonkin/shortlink/internal/apperrors"
	"github.com/nikolaykonkin/shortlink/internal/model"
	"github.com/nikolaykonkin/shortlink/internal/repository"
	"github.com/nikolaykonkin/shortlink/pkg/auth"
)

// UserService реализует регистрацию и вход пользователей
type UserService struct {
	users     repository.UserRepository
	jwtSecret []byte
	jwtTTL    time.Duration
}

// NewUserService создает сервис пользователей
func NewUserService(users repository.UserRepository, jwtSecret []byte, jwtTTL time.Duration) *UserService {
	return &UserService{users: users, jwtSecret: jwtSecret, jwtTTL: jwtTTL}
}

// Register регистрирует нового пользователя, возвращая его публичное представление
func (s *UserService) Register(ctx context.Context, req model.UserCreateRequest) (*model.UserResponse, error) {
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return nil, fmt.Errorf("хеширование пароля: %w", err)
	}

	user := &model.User{
		Username: req.Username,
		Email:    req.Email,
		Password: hash,
	}

	if err := s.users.Create(ctx, user); err != nil {
		return nil, err // ErrDuplicateUser от репозитория уже в нужном виде, оборачивать не нужно
	}

	response := user.ToResponse()

	return &response, nil
}

// Login проверяет учетные данные и выдает JWT вместе с моментом его истечения
//
// Отсутствие пользователя и неверный пароль возвращают одну и ту же
// ErrInvalidCredentials — иначе по разнице ответов можно было бы определить,
// какие email вообще зарегистрированы в системе (user enumeration)
func (s *UserService) Login(ctx context.Context, req model.UserLoginRequest) (string, time.Time, error) {
	user, err := s.users.GetByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, apperrors.ErrUserNotFound) {
			return "", time.Time{}, apperrors.ErrInvalidCredentials
		}
		return "", time.Time{}, fmt.Errorf("поиск пользователя: %w", err)
	}

	if err := auth.ComparePassword(user.Password, req.Password); err != nil {
		return "", time.Time{}, apperrors.ErrInvalidCredentials
	}

	token, expiresAt, err := auth.GenerateToken(user.ID, s.jwtSecret, s.jwtTTL)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("выдача токена: %w", err)
	}

	return token, expiresAt, nil
}

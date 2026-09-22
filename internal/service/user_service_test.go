package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nikolaykonkin/shortlink/internal/apperrors"
	"github.com/nikolaykonkin/shortlink/internal/model"
	"github.com/nikolaykonkin/shortlink/internal/repository"
	"github.com/nikolaykonkin/shortlink/pkg/auth"
)

// fakeUserRepository — in-memory реализация repository.UserRepository для тестов
//
// Create и GetByEmail делегируют в функции-поля, как createFunc и getByIDFunc
// у fakeLinkRepository — это позволяет каждому тесту точно задать поведение,
// не завися от того, какой именно код сгенерировал UserService
type fakeUserRepository struct {
	createFunc     func(user *model.User) error
	createCalls    int
	getByEmailFunc func(email string) (*model.User, error)
}

func (f *fakeUserRepository) Create(_ context.Context, user *model.User) error {
	f.createCalls++
	return f.createFunc(user)
}

func (f *fakeUserRepository) GetByID(_ context.Context, _ int64) (*model.User, error) {
	return nil, errors.New("не используется в этих тестах")
}

func (f *fakeUserRepository) GetByEmail(_ context.Context, email string) (*model.User, error) {
	return f.getByEmailFunc(email)
}

var _ repository.UserRepository = (*fakeUserRepository)(nil)

const jwtTTL = time.Hour

func TestUserService_Register_Success(t *testing.T) {
	var savedUser *model.User
	repo := &fakeUserRepository{
		createFunc: func(user *model.User) error {
			user.ID = 1
			savedUser = user
			return nil
		},
	}
	svc := NewUserService(repo, []byte("secret"), jwtTTL)

	resp, err := svc.Register(context.Background(), model.UserCreateRequest{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "secret123",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 1, repo.createCalls)

	// пароль в репозиторий должен уйти хешированным, а не как есть
	require.NotNil(t, savedUser)
	assert.NotEqual(t, "secret123", savedUser.Password)
	assert.NoError(t, auth.ComparePassword(savedUser.Password, "secret123"))

	// в ответе пароля нет вовсе — ни как поля, ни в сериализованном виде
	assert.Equal(t, model.UserResponse{ID: 1, Username: "alice", Email: "alice@example.com"}, *resp)

	body, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "password")
	assert.NotContains(t, string(body), savedUser.Password)
}

func TestUserService_Register_PropagatesDuplicateUser(t *testing.T) {
	repo := &fakeUserRepository{
		createFunc: func(_ *model.User) error {
			return apperrors.ErrDuplicateUser
		},
	}
	svc := NewUserService(repo, []byte("secret"), jwtTTL)

	resp, err := svc.Register(context.Background(), model.UserCreateRequest{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "secret123",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, apperrors.ErrDuplicateUser)
}

func TestUserService_Register_HashingError(t *testing.T) {
	repo := &fakeUserRepository{
		createFunc: func(_ *model.User) error {
			t.Fatal("Create не должен вызываться, если хеширование не удалось")
			return nil
		},
	}
	svc := NewUserService(repo, []byte("secret"), jwtTTL)

	// bcrypt отказывается хешировать пароль длиннее 72 байт — это настоящая ошибка
	// хеширования, а не смоделированная, поэтому fakeUserRepository для нее не нужен
	tooLong := strings.Repeat("a", 73)

	resp, err := svc.Register(context.Background(), model.UserCreateRequest{
		Username: "alice",
		Email:    "alice@example.com",
		Password: tooLong,
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Zero(t, repo.createCalls)
}

func TestUserService_Login_Success(t *testing.T) {
	hash, err := auth.HashPassword("secret123")
	require.NoError(t, err)

	repo := &fakeUserRepository{
		getByEmailFunc: func(_ string) (*model.User, error) {
			return &model.User{ID: 1, Email: "alice@example.com", Password: hash}, nil
		},
	}
	svc := NewUserService(repo, []byte("secret"), jwtTTL)

	before := time.Now()
	token, expiresAt, err := svc.Login(context.Background(), model.UserLoginRequest{
		Email:    "alice@example.com",
		Password: "secret123",
	})

	require.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.True(t, expiresAt.After(before))
}

func TestUserService_Login_UnknownEmailReturnsInvalidCredentials(t *testing.T) {
	repo := &fakeUserRepository{
		getByEmailFunc: func(_ string) (*model.User, error) {
			return nil, apperrors.ErrUserNotFound
		},
	}
	svc := NewUserService(repo, []byte("secret"), jwtTTL)

	token, _, err := svc.Login(context.Background(), model.UserLoginRequest{
		Email:    "ghost@example.com",
		Password: "whatever123",
	})

	require.Error(t, err)
	assert.Empty(t, token)
	// именно ErrInvalidCredentials, а не ErrUserNotFound — иначе по разнице ответов
	// можно было бы определить, какие email вообще зарегистрированы (user enumeration)
	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)
}

func TestUserService_Login_WrongPasswordReturnsInvalidCredentials(t *testing.T) {
	hash, err := auth.HashPassword("secret123")
	require.NoError(t, err)

	repo := &fakeUserRepository{
		getByEmailFunc: func(_ string) (*model.User, error) {
			return &model.User{ID: 1, Email: "alice@example.com", Password: hash}, nil
		},
	}
	svc := NewUserService(repo, []byte("secret"), jwtTTL)

	token, _, err := svc.Login(context.Background(), model.UserLoginRequest{
		Email:    "alice@example.com",
		Password: "wrong-password",
	})

	require.Error(t, err)
	assert.Empty(t, token)
	// та же самая ошибка, что и при несуществующем email — проверяем именно через
	// errors.Is, а не по тексту: текст совпадает у обоих случаев, но важна идентичность
	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)
}

func TestUserService_Login_PropagatesUnrelatedRepositoryError(t *testing.T) {
	repoErr := errors.New("сбой соединения с базой")
	repo := &fakeUserRepository{
		getByEmailFunc: func(_ string) (*model.User, error) {
			return nil, repoErr
		},
	}
	svc := NewUserService(repo, []byte("secret"), jwtTTL)

	token, _, err := svc.Login(context.Background(), model.UserLoginRequest{
		Email:    "alice@example.com",
		Password: "secret123",
	})

	require.Error(t, err)
	assert.Empty(t, token)
	assert.ErrorIs(t, err, repoErr)
	assert.NotErrorIs(t, err, apperrors.ErrInvalidCredentials)
}

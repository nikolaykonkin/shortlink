package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nikolaykonkin/shortlink/internal/apperrors"
	"github.com/nikolaykonkin/shortlink/internal/model"
	"github.com/nikolaykonkin/shortlink/internal/repository"
	"github.com/nikolaykonkin/shortlink/internal/service"
	"github.com/nikolaykonkin/shortlink/pkg/auth"
)

// stubUserRepository — in-memory реализация repository.UserRepository для тестов хендлера
//
// Такой же по духу fake, что и в internal/service (createFunc/getByEmailFunc), но заведен
// отдельно: тесты хендлера гоняют настоящий UserService, а не подменяют его целиком,
// и репозиторий — единственная точка, которую нужно застабить
type stubUserRepository struct {
	createFunc     func(user *model.User) error
	createCalls    int
	getByEmailFunc func(email string) (*model.User, error)
}

func (s *stubUserRepository) Create(_ context.Context, user *model.User) error {
	s.createCalls++
	return s.createFunc(user)
}

func (s *stubUserRepository) GetByID(_ context.Context, _ int64) (*model.User, error) {
	return nil, errors.New("не используется в этих тестах")
}

func (s *stubUserRepository) GetByEmail(_ context.Context, email string) (*model.User, error) {
	return s.getByEmailFunc(email)
}

var _ repository.UserRepository = (*stubUserRepository)(nil)

// newAuthHandler собирает AuthHandler поверх настоящего UserService — тест проверяет
// связку хендлер+сервис+хеширование пароля, а не хендлер в вакууме
func newAuthHandler(repo *stubUserRepository) *AuthHandler {
	return NewAuthHandler(service.NewUserService(repo, []byte(testJWTSecret), testJWTTTL))
}

func TestAuthHandler_Register_Success(t *testing.T) {
	repo := &stubUserRepository{
		createFunc: func(user *model.User) error {
			user.ID = 1
			return nil
		},
	}
	h := newAuthHandler(repo)

	body := strings.NewReader(`{"username":"alice","email":"alice@example.com","password":"secret123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/register", body)
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, 1, repo.createCalls)

	resp := decodeJSON[model.UserResponse](t, rec)
	assert.Equal(t, int64(1), resp.ID)
	assert.Equal(t, "alice", resp.Username)
	assert.Equal(t, "alice@example.com", resp.Email)
	assert.NotContains(t, rec.Body.String(), "password")
}

func TestAuthHandler_Register_InvalidBody(t *testing.T) {
	repo := &stubUserRepository{
		createFunc: func(_ *model.User) error {
			t.Fatal("Create не должен вызываться при некорректном теле запроса")
			return nil
		},
	}
	h := newAuthHandler(repo)

	req := httptest.NewRequest(http.MethodPost, "/api/register", strings.NewReader(`{`))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Zero(t, repo.createCalls)
}

func TestAuthHandler_Register_DuplicateEmail(t *testing.T) {
	repo := &stubUserRepository{
		createFunc: func(_ *model.User) error {
			return apperrors.ErrDuplicateUser
		},
	}
	h := newAuthHandler(repo)

	body := strings.NewReader(`{"username":"alice","email":"alice@example.com","password":"secret123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/register", body)
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestAuthHandler_Login_Success(t *testing.T) {
	hash, err := auth.HashPassword("secret123")
	require.NoError(t, err)

	repo := &stubUserRepository{
		getByEmailFunc: func(_ string) (*model.User, error) {
			return &model.User{ID: 1, Email: "alice@example.com", Password: hash}, nil
		},
	}
	h := newAuthHandler(repo)

	before := time.Now()
	body := strings.NewReader(`{"email":"alice@example.com","password":"secret123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	resp := decodeJSON[loginResponse](t, rec)
	assert.NotEmpty(t, resp.Token)
	assert.True(t, resp.ExpiresAt.After(before))
}

func TestAuthHandler_Login_InvalidCredentials(t *testing.T) {
	repo := &stubUserRepository{
		getByEmailFunc: func(_ string) (*model.User, error) {
			return nil, apperrors.ErrUserNotFound
		},
	}
	h := newAuthHandler(repo)

	body := strings.NewReader(`{"email":"ghost@example.com","password":"whatever123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

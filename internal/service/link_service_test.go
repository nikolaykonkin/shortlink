package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nikolaykonkin/shortlink/internal/apperrors"
	"github.com/nikolaykonkin/shortlink/internal/cache"
	"github.com/nikolaykonkin/shortlink/internal/model"
	"github.com/nikolaykonkin/shortlink/internal/repository"
)

// fakeLinkRepository — in-memory реализация repository.LinkRepository для тестов
//
// Create делегирует в createFunc с номером попытки — это позволяет каждому тесту точно задать
// поведение по счету вызова, не завися от того, какой именно код сгенерировал LinkService
type fakeLinkRepository struct {
	createFunc  func(attempt int, link *model.Link) error
	createCalls int
}

func (f *fakeLinkRepository) Create(_ context.Context, link *model.Link) error {
	f.createCalls++
	return f.createFunc(f.createCalls, link)
}

func (f *fakeLinkRepository) GetByShortCode(_ context.Context, _ string) (*model.Link, error) {
	return nil, errors.New("не используется в этих тестах")
}

func (f *fakeLinkRepository) GetByID(_ context.Context, _ int64) (*model.Link, error) {
	return nil, errors.New("не используется в этих тестах")
}

func (f *fakeLinkRepository) Delete(_ context.Context, _ int64) error {
	return errors.New("не используется в этих тестах")
}

func (f *fakeLinkRepository) DeleteExpired(_ context.Context, _ time.Time) (int64, error) {
	return 0, errors.New("не используется в этих тестах")
}

var _ repository.LinkRepository = (*fakeLinkRepository)(nil)

// fakeClickRecorder — заглушка ClickRecorder для тестов, не связанных с подсчетом кликов
type fakeClickRecorder struct{}

func (f *fakeClickRecorder) Record(_ int64) {}

func TestLinkService_Create_Success(t *testing.T) {
	repo := &fakeLinkRepository{
		createFunc: func(_ int, link *model.Link) error {
			link.ID = 1
			return nil
		},
	}
	svc := NewLinkService(repo, &fakeClickRecorder{}, cache.NewMemoryCache())

	resp, err := svc.Create(context.Background(), 42, model.LinkCreateRequest{OriginalURL: "https://example.com"})

	require.NoError(t, err)
	assert.Equal(t, 1, repo.createCalls)
	assert.Len(t, resp.ShortCode, shortCodeLength)
}

func TestLinkService_Create_RetriesOnCollision(t *testing.T) {
	repo := &fakeLinkRepository{
		createFunc: func(attempt int, link *model.Link) error {
			if attempt < 3 {
				return apperrors.ErrDuplicateShortCode
			}
			link.ID = 1
			return nil
		},
	}
	svc := NewLinkService(repo, &fakeClickRecorder{}, cache.NewMemoryCache())

	resp, err := svc.Create(context.Background(), 42, model.LinkCreateRequest{OriginalURL: "https://example.com"})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 3, repo.createCalls)
}

func TestLinkService_Create_ExhaustsAttempts(t *testing.T) {
	repo := &fakeLinkRepository{
		createFunc: func(_ int, _ *model.Link) error {
			return apperrors.ErrDuplicateShortCode
		},
	}
	svc := NewLinkService(repo, &fakeClickRecorder{}, cache.NewMemoryCache())

	resp, err := svc.Create(context.Background(), 42, model.LinkCreateRequest{OriginalURL: "https://example.com"})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, apperrors.ErrShortCodeSpaceExhausted)
	assert.Equal(t, maxAttempts, repo.createCalls)
}

func TestLinkService_Create_CustomAliasCollisionDoesNotRetry(t *testing.T) {
	repo := &fakeLinkRepository{
		createFunc: func(_ int, _ *model.Link) error {
			return apperrors.ErrDuplicateShortCode
		},
	}
	svc := NewLinkService(repo, &fakeClickRecorder{}, cache.NewMemoryCache())

	resp, err := svc.Create(context.Background(), 42, model.LinkCreateRequest{
		OriginalURL: "https://example.com",
		CustomAlias: "my-alias",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, apperrors.ErrDuplicateShortCode)
	assert.NotErrorIs(t, err, apperrors.ErrShortCodeSpaceExhausted)
	assert.Equal(t, 1, repo.createCalls)
}

func TestLinkService_Create_PropagatesUnrelatedRepositoryError(t *testing.T) {
	repoErr := errors.New("сбой соединения с базой")
	repo := &fakeLinkRepository{
		createFunc: func(_ int, _ *model.Link) error {
			return repoErr
		},
	}
	svc := NewLinkService(repo, &fakeClickRecorder{}, cache.NewMemoryCache())

	resp, err := svc.Create(context.Background(), 42, model.LinkCreateRequest{OriginalURL: "https://example.com"})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, repoErr)
	assert.Equal(t, 1, repo.createCalls)
}

func TestGenerateShortCode(t *testing.T) {
	code, err := generateShortCode(shortCodeLength)

	require.NoError(t, err)
	assert.Len(t, code, shortCodeLength)
	for _, ch := range code {
		assert.Contains(t, base62Alphabet, string(ch))
	}
}

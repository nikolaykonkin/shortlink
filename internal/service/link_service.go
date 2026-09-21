package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/nikolaykonkin/shortlink/internal/apperrors"
	"github.com/nikolaykonkin/shortlink/internal/cache"
	"github.com/nikolaykonkin/shortlink/internal/model"
	"github.com/nikolaykonkin/shortlink/internal/repository"
)

const (
	shortCodeLength = 7
	maxAttempts     = 5
	base62Alphabet  = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

// ClickRecorder регистрирует переход по ссылке асинхронно, не блокируя вызывающий код
type ClickRecorder interface {
	Record(linkID int64)
}

// ClickStats отдает накопленное число переходов по ссылке
//
// Отдельный от ClickRecorder интерфейс: запись асинхронна и живет в воркере,
// а чтение — синхронный запрос в БД, и реализуют их разные компоненты
type ClickStats interface {
	CountByLinkID(ctx context.Context, linkID int64) (int64, error)
}

// LinkService реализует создание коротких ссылок
type LinkService struct {
	links      repository.LinkRepository
	clicks     ClickRecorder
	clickStats ClickStats
	cache      cache.Cacher
}

// NewLinkService создает сервис ссылок
func NewLinkService(links repository.LinkRepository, clicks ClickRecorder, clickStats ClickStats, linkCache cache.Cacher) *LinkService {
	return &LinkService{links: links, clicks: clicks, clickStats: clickStats, cache: linkCache}
}

// Create создает короткую ссылку для req.OriginalURL, привязанную к userID
//
// Если req.CustomAlias задан, используется он: конфликт по нему — это
// занятый пользователем алиас, а не повод для повторной генерации
// Автогенерируемый код при коллизии перегенерируется до maxAttempts раз
func (s *LinkService) Create(ctx context.Context, userID int64, req model.LinkCreateRequest) (*model.LinkResponse, error) {
	if req.CustomAlias != "" {
		return s.createWithCode(ctx, userID, req, req.CustomAlias)
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		code, err := generateShortCode(shortCodeLength)
		if err != nil {
			return nil, fmt.Errorf("генерация короткого кода: %w", err)
		}

		response, err := s.createWithCode(ctx, userID, req, code)
		if err == nil {
			return response, nil
		}
		if !errors.Is(err, apperrors.ErrDuplicateShortCode) {
			return nil, err
		}
	}

	log.Printf("не удалось подобрать свободный короткий код за %d попыток (userID=%d)", maxAttempts, userID)

	return nil, apperrors.ErrShortCodeSpaceExhausted
}

// createWithCode пытается создать ссылку с конкретным кодом code
func (s *LinkService) createWithCode(
	ctx context.Context, userID int64, req model.LinkCreateRequest, code string,
) (*model.LinkResponse, error) {
	link := &model.Link{
		ShortCode:   code,
		OriginalURL: req.OriginalURL,
		UserID:      userID,
		ExpiresAt:   req.ExpiresAt,
	}

	if err := s.links.Create(ctx, link); err != nil {
		return nil, err
	}

	response := link.ToResponse()

	return &response, nil
}

// Delete удаляет ссылку linkID, если она принадлежит userID
func (s *LinkService) Delete(ctx context.Context, userID, linkID int64) error {
	link, err := s.links.GetByID(ctx, linkID)
	if err != nil {
		return err
	}

	if link.UserID != userID {
		return apperrors.ErrForbidden
	}

	if err := s.links.Delete(ctx, linkID); err != nil {
		return err
	}

	s.cache.Delete(link.ShortCode)

	return nil
}

// Stats возвращает статистику переходов по ссылке linkID, если она принадлежит userID
//
// В счетчик попадают только клики, уже записанные в БД: то, что еще лежит
// в буфере воркера, появится после ближайшего сброса
func (s *LinkService) Stats(ctx context.Context, userID, linkID int64) (*model.LinkStatsResponse, error) {
	link, err := s.links.GetByID(ctx, linkID)
	if err != nil {
		return nil, err
	}

	if link.UserID != userID {
		return nil, apperrors.ErrForbidden
	}

	count, err := s.clickStats.CountByLinkID(ctx, linkID)
	if err != nil {
		return nil, err
	}

	return &model.LinkStatsResponse{
		LinkID:     link.ID,
		ShortCode:  link.ShortCode,
		ClickCount: count,
	}, nil
}

// Resolve возвращает ссылку по короткому коду для редиректа
//
// Просроченная ссылка (expires_at в прошлом) возвращается как ErrLinkNotFound,
// не дожидаясь фонового воркера очистки — иначе редирект продолжал бы
// работать до случайного момента, когда воркер дойдет до этой строки
func (s *LinkService) Resolve(ctx context.Context, shortCode string) (*model.Link, error) {
	if link, ok := s.cache.Get(shortCode); ok {
		if isExpired(link) {
			return nil, apperrors.ErrLinkNotFound
		}
		return link, nil
	}

	link, err := s.links.GetByShortCode(ctx, shortCode)
	if err != nil {
		return nil, err
	}

	if isExpired(link) {
		return nil, apperrors.ErrLinkNotFound
	}

	s.cache.Set(shortCode, link)

	return link, nil
}

// isExpired проверяется и для попадания в кэш, и для промаха: запись в кэше не узнает
// о наступлении своего expires_at сама, поэтому срок годности пересчитывается при каждом обращении
func isExpired(link *model.Link) bool {
	return link.ExpiresAt != nil && link.ExpiresAt.Before(time.Now())
}

// RecordClick регистрирует переход по ссылке linkID — отдельно от Resolve,
// чтобы поиск ссылки оставался чистой операцией без побочных эффектов
func (s *LinkService) RecordClick(linkID int64) {
	s.clicks.Record(linkID)
}

// generateShortCode возвращает случайную строку длины n в алфавите base62
func generateShortCode(n int) (string, error) {
	randomBytes := make([]byte, n)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	code := make([]byte, n)
	for i, b := range randomBytes {
		code[i] = base62Alphabet[int(b)%len(base62Alphabet)]
	}

	return string(code), nil
}

package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/nikolaykonkin/shortlink/internal/apperrors"
	"github.com/nikolaykonkin/shortlink/internal/model"
	"github.com/nikolaykonkin/shortlink/internal/repository"
)

const (
	shortCodeLength = 7
	maxAttempts     = 5
	base62Alphabet  = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

// LinkService реализует создание коротких ссылок
type LinkService struct {
	links repository.LinkRepository
}

// NewLinkService создает сервис ссылок
func NewLinkService(links repository.LinkRepository) *LinkService {
	return &LinkService{links: links}
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

	return s.links.Delete(ctx, linkID)
}

// Resolve возвращает ссылку по короткому коду для редиректа
//
// Просроченная ссылка (expires_at в прошлом) возвращается как ErrLinkNotFound,
// не дожидаясь фонового воркера очистки — иначе редирект продолжал бы
// работать до случайного момента, когда воркер дойдет до этой строки
func (s *LinkService) Resolve(ctx context.Context, shortCode string) (*model.Link, error) {
	link, err := s.links.GetByShortCode(ctx, shortCode)
	if err != nil {
		return nil, err
	}

	if link.ExpiresAt != nil && link.ExpiresAt.Before(time.Now()) {
		return nil, apperrors.ErrLinkNotFound
	}

	return link, nil
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

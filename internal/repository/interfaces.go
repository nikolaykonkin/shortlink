package repository

import (
	"context"
	"time"

	"github.com/nikolaykonkin/shortlink/internal/model"
)

// UserRepository описывает доступ к хранилищу пользователей
// Реализация не содержит бизнес-правил — только чтение и запись
type UserRepository interface {
	Create(ctx context.Context, user *model.User) error
	GetByID(ctx context.Context, id int64) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
}

// LinkRepository описывает доступ к хранилищу ссылок
type LinkRepository interface {
	Create(ctx context.Context, link *model.Link) error
	GetByShortCode(ctx context.Context, shortCode string) (*model.Link, error)
	GetByID(ctx context.Context, id int64) (*model.Link, error)
	Delete(ctx context.Context, id int64) error
	// DeleteExpired удаляет ссылки с истекшим сроком жизни и возвращает их количество
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

// ClickRepository описывает доступ к хранилищу переходов по ссылкам
type ClickRepository interface {
	Create(ctx context.Context, click *model.Click) error
	CreateBatch(ctx context.Context, linkIDs []int64) error
	CountByLinkID(ctx context.Context, linkID int64) (int64, error)
}

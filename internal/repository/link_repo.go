package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nikolaykonkin/shortlink/internal/apperrors"
	"github.com/nikolaykonkin/shortlink/internal/model"
)

// PostgresLinkRepository реализует LinkRepository поверх pgxpool
type PostgresLinkRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresLinkRepository создает репозиторий ссылок на Postgres
func NewPostgresLinkRepository(pool *pgxpool.Pool) *PostgresLinkRepository {
	return &PostgresLinkRepository{pool: pool}
}

// компиляция упадет, если PostgresLinkRepository перестанет реализовывать интерфейс
var _ LinkRepository = (*PostgresLinkRepository)(nil)

func (r *PostgresLinkRepository) Create(ctx context.Context, link *model.Link) error {
	const query = `
		INSERT INTO links (short_code, original_url, user_id, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`

	err := r.pool.QueryRow(ctx, query, link.ShortCode, link.OriginalURL, link.UserID, link.ExpiresAt).
		Scan(&link.ID, &link.CreatedAt, &link.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrDuplicateShortCode
		}
		return fmt.Errorf("создание ссылки: %w", err)
	}

	return nil
}

func (r *PostgresLinkRepository) GetByShortCode(ctx context.Context, shortCode string) (*model.Link, error) {
	const query = `
		SELECT id, short_code, original_url, user_id, expires_at, created_at, updated_at
		FROM links
		WHERE short_code = $1
	`

	var link model.Link
	err := r.pool.QueryRow(ctx, query, shortCode).Scan(
		&link.ID, &link.ShortCode, &link.OriginalURL, &link.UserID,
		&link.ExpiresAt, &link.CreatedAt, &link.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.ErrLinkNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("поиск ссылки по короткому коду: %w", err)
	}

	return &link, nil
}

func (r *PostgresLinkRepository) GetByID(ctx context.Context, id int64) (*model.Link, error) {
	const query = `
		SELECT id, short_code, original_url, user_id, expires_at, created_at, updated_at
		FROM links
		WHERE id = $1
	`

	var link model.Link
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&link.ID, &link.ShortCode, &link.OriginalURL, &link.UserID,
		&link.ExpiresAt, &link.CreatedAt, &link.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.ErrLinkNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("поиск ссылки по id: %w", err)
	}

	return &link, nil
}

func (r *PostgresLinkRepository) Delete(ctx context.Context, id int64) error {
	const query = `DELETE FROM links WHERE id = $1`

	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("удаление ссылки: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.ErrLinkNotFound
	}

	return nil
}

func (r *PostgresLinkRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	const query = `DELETE FROM links WHERE expires_at IS NOT NULL AND expires_at < $1`

	tag, err := r.pool.Exec(ctx, query, before)
	if err != nil {
		return 0, fmt.Errorf("удаление просроченных ссылок: %w", err)
	}

	return tag.RowsAffected(), nil
}

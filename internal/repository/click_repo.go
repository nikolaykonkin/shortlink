package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nikolaykonkin/shortlink/internal/model"
)

// PostgresClickRepository реализует ClickRepository поверх pgxpool
type PostgresClickRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresClickRepository создает репозиторий кликов на Postgres
func NewPostgresClickRepository(pool *pgxpool.Pool) *PostgresClickRepository {
	return &PostgresClickRepository{pool: pool}
}

// проверка на этапе компиляции, что PostgresClickRepository реализует интерфейс
var _ ClickRepository = (*PostgresClickRepository)(nil)

func (r *PostgresClickRepository) Create(ctx context.Context, click *model.Click) error {
	const query = `
		INSERT INTO clicks (link_id)
		VALUES ($1)
		RETURNING id, clicked_at
	`

	err := r.pool.QueryRow(ctx, query, click.LinkID).Scan(&click.ID, &click.ClickedAt)
	if err != nil {
		return fmt.Errorf("создание клика: %w", err)
	}

	return nil
}

func (r *PostgresClickRepository) CountByLinkID(ctx context.Context, linkID int64) (int64, error) {
	const query = `SELECT COUNT(*) FROM clicks WHERE link_id = $1`

	var count int64
	if err := r.pool.QueryRow(ctx, query, linkID).Scan(&count); err != nil {
		return 0, fmt.Errorf("подсчет кликов по ссылке: %w", err)
	}

	return count, nil
}

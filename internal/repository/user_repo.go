package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nikolaykonkin/shortlink/internal/apperrors"
	"github.com/nikolaykonkin/shortlink/internal/model"
)

// PostgresUserRepository реализует UserRepository поверх pgxpool
type PostgresUserRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresUserRepository создает репозиторий пользователей на Postgres
func NewPostgresUserRepository(pool *pgxpool.Pool) *PostgresUserRepository {
	return &PostgresUserRepository{pool: pool}
}

// компиляция упадет, если PostgresUserRepository перестанет реализовывать интерфейс
var _ UserRepository = (*PostgresUserRepository)(nil)

func (r *PostgresUserRepository) Create(ctx context.Context, user *model.User) error {
	const query = `
		INSERT INTO users (username, email, password)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at
	`

	err := r.pool.QueryRow(ctx, query, user.Username, user.Email, user.Password).
		Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrDuplicateUser
		}
		return fmt.Errorf("создание пользователя: %w", err)
	}

	return nil
}

func (r *PostgresUserRepository) GetByID(ctx context.Context, id int64) (*model.User, error) {
	const query = `
		SELECT id, username, email, password, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	var user model.User
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Username, &user.Email, &user.Password, &user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("поиск пользователя по id: %w", err)
	}

	return &user, nil
}

func (r *PostgresUserRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	const query = `
		SELECT id, username, email, password, created_at, updated_at
		FROM users
		WHERE email = $1
	`

	var user model.User
	err := r.pool.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Username, &user.Email, &user.Password, &user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("поиск пользователя по email: %w", err)
	}

	return &user, nil
}

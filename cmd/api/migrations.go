package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// applyMigrations применяет непримененные ранее *.up.sql файлы из dir по возрастанию имени
// Учет примененных миграций ведется в таблице schema_migrations
func applyMigrations(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`)
	if err != nil {
		return fmt.Errorf("создание таблицы schema_migrations: %w", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return fmt.Errorf("поиск файлов миграций: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("не найдено ни одного файла миграций в %s", dir)
	}
	sort.Strings(files)

	for _, file := range files {
		version := filepath.Base(file)

		var applied bool
		err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, version).
			Scan(&applied)
		if err != nil {
			return fmt.Errorf("проверка миграции %s: %w", version, err)
		}
		if applied {
			continue
		}

		if err := applyOne(ctx, pool, file, version); err != nil {
			return err
		}

		log.Printf("применена миграция %s", version)
	}

	return nil
}

// applyOne выполняет SQL миграции и запись в schema_migrations одной транзакцией,
// чтобы падение на середине файла не оставляло базу и учет миграций рассинхронизированными
func applyOne(ctx context.Context, pool *pgxpool.Pool, file, version string) error {
	sqlBytes, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("чтение файла %s: %w", file, err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("старт транзакции для %s: %w", version, err)
	}
	defer tx.Rollback(ctx) // если Commit уже прошел, Rollback здесь просто no-op

	if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("выполнение миграции %s: %w", version, err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return fmt.Errorf("запись миграции %s: %w", version, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("коммит миграции %s: %w", version, err)
	}

	return nil
}

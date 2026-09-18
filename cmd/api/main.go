package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/nikolaykonkin/shortlink/internal/handler"
	"github.com/nikolaykonkin/shortlink/internal/repository"
	"github.com/nikolaykonkin/shortlink/internal/service"
	"github.com/nikolaykonkin/shortlink/pkg/database"
)

// jwtTTL — константа, не переменная окружения: срок жизни токена не зависит от окружения,
// в отличие от адреса базы и секрета
const jwtTTL = 24 * time.Hour

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("не задана переменная окружения DATABASE_URL")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("не задана переменная окружения JWT_SECRET")
	}

	ctx := context.Background()

	pool, err := database.New(ctx, database.Config{DSN: dsn})
	if err != nil {
		log.Fatalf("подключение к базе данных: %v", err)
	}
	defer pool.Close()

	if applyMigrationsOnStart() {
		if err := applyMigrations(ctx, pool, "migrations"); err != nil {
			log.Fatalf("применение миграций: %v", err)
		}
	} else {
		log.Print("автоприменение миграций отключено (APPLY_MIGRATIONS_ON_START=false)")
	}

	userRepo := repository.NewPostgresUserRepository(pool)
	userService := service.NewUserService(userRepo, []byte(jwtSecret), jwtTTL)
	authHandler := handler.NewAuthHandler(userService)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", healthHandler)
	mux.HandleFunc("POST /api/register", authHandler.Register)
	mux.HandleFunc("POST /api/login", authHandler.Login)

	log.Printf("shortlink стартует на :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("сервер завершился с ошибкой: %v", err)
	}
}

// applyMigrationsOnStart решает, применять ли миграции при старте (по умолчанию true)
// Отключается переменной APPLY_MIGRATIONS_ON_START=false, если миграции применяются вне запуска сервиса
func applyMigrationsOnStart() bool {
	value := os.Getenv("APPLY_MIGRATIONS_ON_START")
	if value == "" {
		return true
	}

	apply, err := strconv.ParseBool(value)
	if err != nil {
		log.Printf("некорректное значение APPLY_MIGRATIONS_ON_START=%q, использую true", value)
		return true
	}

	return apply
}

// healthHandler — базовый health-check, чтобы убедиться, что сервис жив
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/nikolaykonkin/shortlink/internal/cache"
	"github.com/nikolaykonkin/shortlink/internal/handler"
	"github.com/nikolaykonkin/shortlink/internal/middleware"
	"github.com/nikolaykonkin/shortlink/internal/repository"
	"github.com/nikolaykonkin/shortlink/internal/service"
	"github.com/nikolaykonkin/shortlink/internal/worker"
	"github.com/nikolaykonkin/shortlink/pkg/database"
)

// jwtTTL — константа, не переменная окружения: срок жизни токена не зависит от окружения,
// в отличие от адреса базы и секрета
const jwtTTL = 24 * time.Hour

// shutdownTimeout — сколько ждать завершения уже идущих запросов при остановке
// Если клиент завис и не отпускает соединение, Shutdown без таймаута ждал бы вечно;
// 10 секунд — компромисс между тем, чтобы не оборвать нормальные запросы,
// и тем, чтобы не блокировать остановку контейнера на неопределенный срок
const shutdownTimeout = 10 * time.Second

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

	// signalCtx — только для того, чтобы дождаться SIGINT/SIGTERM и начать остановку
	// Фоновые операции (пул, воркер, планировщик) используют отдельный context.Background():
	// если бы они разделяли signalCtx, получение сигнала само отменило бы их контекст раньше,
	// чем мы успели бы слить буфер кликов и корректно остановить HTTP-сервер,
	// а финальный флаш воркера делал бы запросы к БД с уже отмененным контекстом
	signalCtx, stopSignalNotify := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignalNotify()

	ctx := context.Background()

	pool, err := database.New(ctx, database.Config{DSN: dsn})
	if err != nil {
		log.Fatalf("подключение к базе данных: %v", err)
	}

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

	const (
		clickBatchSize     = 50
		clickFlushInterval = 5 * time.Second
	)

	clickRepo := repository.NewPostgresClickRepository(pool)
	clickWorker := worker.NewClickWorker(clickRepo, clickBatchSize, clickFlushInterval)
	go clickWorker.Run(ctx)

	linkRepo := repository.NewPostgresLinkRepository(pool)

	const expiredLinksPurgeInterval = 10 * time.Minute

	scheduler := worker.NewScheduler(linkRepo, expiredLinksPurgeInterval)
	go scheduler.Run(ctx)

	linkCache := cache.NewMemoryCache()
	linkService := service.NewLinkService(linkRepo, clickWorker, clickRepo, linkCache)
	linkHandler := handler.NewLinkHandler(linkService)

	requireAuth := middleware.AuthMiddleware([]byte(jwtSecret))

	const (
		linkCreateLimit  = 10
		linkCreateWindow = time.Minute
	)

	rateLimiter := middleware.NewRateLimiter(linkCreateLimit, linkCreateWindow)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", healthHandler)
	mux.HandleFunc("POST /api/register", authHandler.Register)
	mux.HandleFunc("POST /api/login", authHandler.Login)
	mux.Handle("POST /api/links", requireAuth(rateLimiter.Middleware(http.HandlerFunc(linkHandler.Create))))
	mux.Handle("DELETE /api/links/{id}", requireAuth(http.HandlerFunc(linkHandler.Delete)))
	mux.Handle("GET /api/links/{id}/stats", requireAuth(http.HandlerFunc(linkHandler.Stats)))
	mux.HandleFunc("GET /{shortCode}", linkHandler.Redirect)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("shortlink стартует на :%s", port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// ждем либо сигнал остановки, либо падение сервера: во втором случае Shutdown
	// ниже не сделает ничего (слушатель уже мертв), но остановка воркера, планировщика
	// и пула все равно должна пройти, а не потеряться в defer, который не выполнится после log.Fatal
	select {
	case <-signalCtx.Done():
		log.Print("получен сигнал остановки, начинаю graceful shutdown")
	case err := <-serverErr:
		log.Printf("сервер завершился с ошибкой: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// порядок остановки важен и намеренно последовательный, не параллельный:
	//
	// 1. HTTP-сервер: перестает принимать новые запросы первым. Иначе Redirect мог бы успеть вызвать
	//    clickWorker.Record после того, как воркер уже закрыл свою очередь,
	//    что привело бы к панике на отправке в закрытый канал
	// 2. clickWorker: к этому моменту новых кликов уже не поступает, и Stop() сливает в БД
	//    финальный буфер один раз, а не гоняется за потоком, который все еще растет
	// 3. scheduler: останавливается последним, потому что это не критично: недочищенные
	//    просроченные ссылки удалятся на первом тике после следующего запуска сервиса
	// 4. pool.Close(): только после того, как оба воркера гарантированно закончили свои последние
	//    обращения к БД, иначе их финальные запросы попали бы в закрытый пул
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown HTTP-сервера: %v", err)
	}

	clickWorker.Stop()
	scheduler.Stop()

	pool.Close()
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

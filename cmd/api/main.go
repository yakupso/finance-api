// Команда api запускает HTTP-сервер планировщика личных финансов.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"finance-api/internal/config"
	"finance-api/internal/repository/postgres"
	"finance-api/internal/service"
	transport "finance-api/internal/transport/http"
)

// healthcheckFlag переводит процесс в режим одноразовой пробы: бинарь
// обращается к собственному /healthz и завершается с соответствующим кодом.
//
// Это нужно, потому что рабочий образ собран на distroless - в нём нет
// ни shell, ни curl, ни wget, которыми обычно описывают HEALTHCHECK.
// Проба тем же бинарём не требует ничего лишнего в образе.
var healthcheckFlag = flag.Bool("healthcheck", false,
	"проверить доступность собственного HTTP-эндпоинта и выйти")

func main() {
	flag.Parse()

	if *healthcheckFlag {
		if err := healthcheck(); err != nil {
			slog.Error("healthcheck failed", slog.Any("error", err))
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		// Логгер может быть ещё не построен (ошибка конфигурации), поэтому
		// пишем через дефолтный.
		slog.Error("service failed to start", slog.Any("error", err))
		os.Exit(1)
	}
}

// healthcheck обращается к /healthz текущего процесса.
//
// Адрес берётся из той же переменной окружения, что и адрес прослушивания,
// поэтому проба не разъедется с настройками сервера.
func healthcheck() error {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	// HTTP_ADDR вида ":8080" означает «все интерфейсы»; изнутри контейнера
	// обращаемся к петлевому.
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("build healthcheck request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("healthcheck request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck returned status %d", resp.StatusCode)
	}
	return nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger(cfg.Log)
	slog.SetDefault(log)

	// Контекст отменяется по SIGINT/SIGTERM: docker stop и Ctrl+C приводят
	// к штатному завершению, а не к обрыву обрабатываемых запросов.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, postgres.PoolConfig{
		URL:            cfg.DB.URL,
		MaxConns:       cfg.DB.MaxConns,
		MinConns:       cfg.DB.MinConns,
		ConnectTimeout: cfg.DB.ConnectTimeout,
	})
	if err != nil {
		return err
	}
	defer pool.Close()
	log.Info("connected to database",
		slog.Int("max_conns", int(cfg.DB.MaxConns)),
		slog.Int("min_conns", int(cfg.DB.MinConns)),
	)

	// Сборка зависимостей: репозитории -> сервисы -> обработчики.
	// DI-контейнер не используется - при таком количестве компонентов явная
	// сборка короче и читается однозначно.
	var (
		categoryRepo  = postgres.NewCategoryRepository(pool)
		operationRepo = postgres.NewOperationRepository(pool)
		statsRepo     = postgres.NewStatsRepository(pool)

		categorySvc  = service.NewCategoryService(categoryRepo)
		operationSvc = service.NewOperationService(operationRepo)
		statsSvc     = service.NewStatsService(statsRepo)

		handlers = transport.NewHandlers(categorySvc, operationSvc, statsSvc, log)
	)

	server := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      transport.NewRouter(handlers),
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
		ErrorLog:     slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("http server started", slog.String("addr", cfg.HTTP.Addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- fmt.Errorf("http server: %w", err)
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received", slog.Duration("timeout", cfg.Shutdown))
	}

	// Даём активным запросам доработать, но не дольше SHUTDOWN_TIMEOUT.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Shutdown)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	log.Info("service stopped")
	return nil
}

func newLogger(cfg config.Log) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.Level}

	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

// Команда migrator применяет миграции базы данных.
//
// Вынесена в отдельный бинарь;
// Для docker-compose это одноразовый сервис, который должен успешно завершиться до запуска API (depends_on: service_completed_successfully)
//
// Использование:
//
//	migrator up            применить все неприменённые миграции
//	migrator up-to <ver>   применить миграции до версии включительно
//	migrator down          откатить последнюю миграцию
//	migrator down-to <ver> откатить до версии включительно
//	migrator reset         откатить все миграции
//	migrator status        показать статус миграций
//	migrator version       показать текущую версию схемы
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"time"

	"finance-api/internal/config"
	"finance-api/migrations"

	_ "github.com/jackc/pgx/v5/stdlib" // драйвер database/sql поверх pgx
	"github.com/pressly/goose/v3"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migrator failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) == 0 {
		return errors.New("команда не указана; ожидается одна из: up, up-to, down, down-to, reset, status, version")
	}
	command, rest := args[0], args[1:]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	goose.SetLogger(log.New(os.Stdout, "", 0))
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	goose.SetBaseFS(migrations.FS)

	db, err := sql.Open("pgx", cfg.DB.URL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.DB.ConnectTimeout)
	defer cancel()
	if err := waitForDB(ctx, db); err != nil {
		return fmt.Errorf("database is not reachable: %w", err)
	}

	// "." - корень встроенной файловой системы migrations.FS.
	const dir = "."

	switch command {
	case "up":
		return goose.UpContext(context.Background(), db, dir)
	case "up-to":
		version, err := parseVersion(rest)
		if err != nil {
			return err
		}
		return goose.UpToContext(context.Background(), db, dir, version)
	case "down":
		return goose.DownContext(context.Background(), db, dir)
	case "down-to":
		version, err := parseVersion(rest)
		if err != nil {
			return err
		}
		return goose.DownToContext(context.Background(), db, dir, version)
	case "reset":
		return goose.ResetContext(context.Background(), db, dir)
	case "status":
		return goose.StatusContext(context.Background(), db, dir)
	case "version":
		return goose.VersionContext(context.Background(), db, dir)
	default:
		return fmt.Errorf("неизвестная команда %q; ожидается одна из: up, up-to, down, down-to, reset, status, version", command)
	}
}

func parseVersion(args []string) (int64, error) {
	if len(args) == 0 {
		return 0, errors.New("не указана версия миграции")
	}
	v, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("версия миграции должна быть числом, получено %q", args[0])
	}
	return v, nil
}

// waitForDB переживает ситуацию, когда контейнер с PostgreSQL уже помечен
// healthy, но соединение ещё не принимается.
func waitForDB(ctx context.Context, db *sql.DB) error {
	const retryInterval = 500 * time.Millisecond

	var lastErr error
	for {
		lastErr = db.PingContext(ctx)
		if lastErr == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (last ping error: %w)", ctx.Err(), lastErr)
		case <-time.After(retryInterval):
		}
	}
}

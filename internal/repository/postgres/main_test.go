//go:build integration

// Интеграционные тесты репозитория выполняются на настоящем PostgreSQL,
// поднятом в контейнере. Мок вместо БД здесь бесполезен: проверяются ровно
// те вещи, которые живут в базе, - составной внешний ключ, регистронезависимый
// уникальный индекс, семантика границ периода и агрегирующий запрос.
//
// Тесты вынесены под тег сборки, чтобы обычный `go test ./...` оставался
// быстрым и не требовал Docker:
//
//	make test              - только unit-тесты
//	make test-integration  - вместе с интеграционными
package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"finance-api/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testPool - пул к контейнеру, общий для всех тестов пакета.
// Контейнер поднимается один раз: запуск PostgreSQL занимает несколько секунд,
// и делать это на каждый тест было бы неоправданно дорого.
var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("finance_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			// Postgres в официальном образе стартует дважды: первый раз -
			// для инициализации кластера. Ждём второго появления строки,
			// иначе можно подключиться к временному экземпляру.
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		log.Fatalf("не удалось запустить контейнер с PostgreSQL: %v", err)
	}

	code := runTests(ctx, container, m)

	if err := testcontainers.TerminateContainer(container); err != nil {
		log.Printf("не удалось остановить контейнер: %v", err)
	}
	os.Exit(code)
}

// runTests вынесен в отдельную функцию, чтобы defer'ы отработали
// до вызова os.Exit в TestMain.
func runTests(ctx context.Context, container *tcpostgres.PostgresContainer, m *testing.M) int {
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Printf("не удалось получить строку подключения: %v", err)
		return 1
	}

	if err := applyMigrations(dsn); err != nil {
		log.Printf("не удалось применить миграции: %v", err)
		return 1
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Printf("не удалось создать пул: %v", err)
		return 1
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Printf("база недоступна: %v", err)
		return 1
	}
	testPool = pool

	return m.Run()
}

// applyMigrations прогоняет те же миграции, что и в проде, - из встроенной
// файловой системы. Так тесты проверяют реальную схему, а не её копию,
// поддерживаемую вручную.
func applyMigrations(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	goose.SetBaseFS(migrations.FS)

	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// truncate очищает таблицы между тестами.
//
// TRUNCATE вместо DELETE: он же сбрасывает последовательности, поэтому
// идентификаторы в каждом тесте начинаются с единицы и их можно сверять точно.
// CASCADE нужен из-за внешнего ключа между operations и categories.
func truncate(t *testing.T) {
	t.Helper()

	_, err := testPool.Exec(context.Background(),
		"TRUNCATE operations, categories RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("не удалось очистить таблицы: %v", err)
	}
}

// testCtx возвращает контекст с таймаутом: зависший запрос должен провалить
// тест, а не заблокировать весь прогон.
func testCtx(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}

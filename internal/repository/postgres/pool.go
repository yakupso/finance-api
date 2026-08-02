// Package postgres содержит реализацию репозиториев поверх PostgreSQL.
//
// ORM не используется намеренно: требование ТЗ выполнять агрегации на стороне
// PostgreSQL предполагает, что SQL написан и контролируется явно. Работа идёт
// через pgx/v5 - нативный драйвер, который, в отличие от database/sql, отдаёт
// типизированные ошибки *pgconn.PgError и позволяет отличить нарушение
// уникальности от нарушения внешнего ключа.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig - параметры пула соединений.
type PoolConfig struct {
	URL            string
	MaxConns       int32
	MinConns       int32
	ConnectTimeout time.Duration
}

// NewPool создаёт пул соединений и дожидается готовности базы.
//
// Ожидание нужно даже при healthcheck'е контейнера: PostgreSQL успевает
// ответить на pg_isready до того, как начнёт принимать обычные соединения.
func NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	if err := waitForPool(pingCtx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database is not reachable: %w", err)
	}
	return pool, nil
}

func waitForPool(ctx context.Context, pool *pgxpool.Pool) error {
	const retryInterval = 500 * time.Millisecond

	var lastErr error
	for {
		lastErr = pool.Ping(ctx)
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

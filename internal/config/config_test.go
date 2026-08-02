package config_test

import (
	"log/slog"
	"testing"
	"time"

	"finance-api/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validDSN = "postgres://user:pass@localhost:5432/db?sslmode=disable"

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()

	// Переменные, влияющие на результат, обнуляются явно: иначе окружение
	// разработчика протекало бы в тест.
	for _, key := range []string{
		"DATABASE_URL", "DB_MAX_CONNS", "DB_MIN_CONNS", "DB_CONNECT_TIMEOUT",
		"HTTP_ADDR", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT",
		"SHUTDOWN_TIMEOUT", "LOG_LEVEL", "LOG_FORMAT",
	} {
		t.Setenv(key, "")
	}
	for key, value := range env {
		t.Setenv(key, value)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": validDSN})

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, validDSN, cfg.DB.URL)
	assert.Equal(t, ":8080", cfg.HTTP.Addr)
	assert.Equal(t, 10*time.Second, cfg.HTTP.ReadTimeout)
	assert.Equal(t, 15*time.Second, cfg.HTTP.WriteTimeout)
	assert.Equal(t, 60*time.Second, cfg.HTTP.IdleTimeout)
	assert.Equal(t, 15*time.Second, cfg.Shutdown)
	assert.Equal(t, int32(10), cfg.DB.MaxConns)
	assert.Equal(t, int32(2), cfg.DB.MinConns)
	assert.Equal(t, slog.LevelInfo, cfg.Log.Level)
	assert.Equal(t, "json", cfg.Log.Format)
}

func TestLoadOverrides(t *testing.T) {
	setEnv(t, map[string]string{
		"DATABASE_URL":       validDSN,
		"HTTP_ADDR":          "127.0.0.1:9000",
		"HTTP_READ_TIMEOUT":  "3s",
		"HTTP_WRITE_TIMEOUT": "1m",
		"SHUTDOWN_TIMEOUT":   "30s",
		"DB_MAX_CONNS":       "50",
		"DB_MIN_CONNS":       "5",
		"LOG_LEVEL":          "debug",
		"LOG_FORMAT":         "text",
	})

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:9000", cfg.HTTP.Addr)
	assert.Equal(t, 3*time.Second, cfg.HTTP.ReadTimeout)
	assert.Equal(t, time.Minute, cfg.HTTP.WriteTimeout)
	assert.Equal(t, 30*time.Second, cfg.Shutdown)
	assert.Equal(t, int32(50), cfg.DB.MaxConns)
	assert.Equal(t, int32(5), cfg.DB.MinConns)
	assert.Equal(t, slog.LevelDebug, cfg.Log.Level)
	assert.Equal(t, "text", cfg.Log.Format)
}

// Некорректная конфигурация должна ронять процесс на старте
func TestLoadInvalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "отсутствует DATABASE_URL",
			env:  map[string]string{},
			want: "DATABASE_URL",
		},
		{
			name: "DATABASE_URL из пробелов",
			env:  map[string]string{"DATABASE_URL": "   "},
			want: "DATABASE_URL",
		},
		{
			name: "DB_MAX_CONNS не число",
			env:  map[string]string{"DATABASE_URL": validDSN, "DB_MAX_CONNS": "много"},
			want: "DB_MAX_CONNS",
		},
		{
			name: "DB_MAX_CONNS меньше единицы",
			env:  map[string]string{"DATABASE_URL": validDSN, "DB_MAX_CONNS": "0"},
			want: "DB_MAX_CONNS",
		},
		{
			name: "DB_MIN_CONNS больше DB_MAX_CONNS",
			env:  map[string]string{"DATABASE_URL": validDSN, "DB_MAX_CONNS": "5", "DB_MIN_CONNS": "10"},
			want: "DB_MIN_CONNS",
		},
		{
			name: "некорректная длительность",
			env:  map[string]string{"DATABASE_URL": validDSN, "HTTP_READ_TIMEOUT": "10 секунд"},
			want: "HTTP_READ_TIMEOUT",
		},
		{
			name: "отрицательная длительность",
			env:  map[string]string{"DATABASE_URL": validDSN, "SHUTDOWN_TIMEOUT": "-5s"},
			want: "SHUTDOWN_TIMEOUT",
		},
		{
			name: "неизвестный уровень логирования",
			env:  map[string]string{"DATABASE_URL": validDSN, "LOG_LEVEL": "verbose"},
			want: "LOG_LEVEL",
		},
		{
			name: "неизвестный формат логов",
			env:  map[string]string{"DATABASE_URL": validDSN, "LOG_FORMAT": "xml"},
			want: "LOG_FORMAT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, tt.env)

			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// Все ошибки конфигурации собираются за один проход: не нужно перезапускать сервис, чтобы узнать про следующую ошибку
func TestLoadReportsAllErrorsAtOnce(t *testing.T) {
	setEnv(t, map[string]string{
		"DB_MAX_CONNS": "abc",
		"LOG_LEVEL":    "verbose",
	})

	_, err := config.Load()
	require.Error(t, err)

	message := err.Error()
	assert.Contains(t, message, "DATABASE_URL")
	assert.Contains(t, message, "DB_MAX_CONNS")
	assert.Contains(t, message, "LOG_LEVEL")
}

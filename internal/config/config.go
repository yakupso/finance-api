// Package config загружает конфигурацию приложения из переменных окружения.
//
// Все ошибки конфигурации собираются и возвращаются разом: при неверном
// окружении процесс падает на старте с полным списком проблем
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config — полная конфигурация сервиса.
type Config struct {
	HTTP     HTTP
	DB       DB
	Log      Log
	Shutdown time.Duration
}

// HTTP — параметры HTTP-сервера.
type HTTP struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// DB — параметры подключения к PostgreSQL.
type DB struct {
	URL            string
	MaxConns       int32
	MinConns       int32
	ConnectTimeout time.Duration
}

// Log — параметры логирования.
type Log struct {
	Level  slog.Level
	Format string // json | text
}

// Load читает конфигурацию из окружения, подставляя значения по умолчанию.
// Единственный обязательный параметр — DATABASE_URL.
func Load() (Config, error) {
	var errs []error
	collect := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	cfg := Config{}

	cfg.HTTP.Addr = envString("HTTP_ADDR", ":8080")
	cfg.HTTP.ReadTimeout = envDuration("HTTP_READ_TIMEOUT", 10*time.Second, collect)
	cfg.HTTP.WriteTimeout = envDuration("HTTP_WRITE_TIMEOUT", 15*time.Second, collect)
	cfg.HTTP.IdleTimeout = envDuration("HTTP_IDLE_TIMEOUT", 60*time.Second, collect)
	cfg.Shutdown = envDuration("SHUTDOWN_TIMEOUT", 15*time.Second, collect)

	cfg.DB.URL = envString("DATABASE_URL", "")
	if strings.TrimSpace(cfg.DB.URL) == "" {
		collect(errors.New("DATABASE_URL is required (например: postgres://user:pass@host:5432/db?sslmode=disable)"))
	}
	cfg.DB.MaxConns = int32(envInt("DB_MAX_CONNS", 10, collect))
	cfg.DB.MinConns = int32(envInt("DB_MIN_CONNS", 2, collect))
	cfg.DB.ConnectTimeout = envDuration("DB_CONNECT_TIMEOUT", 10*time.Second, collect)

	if cfg.DB.MaxConns < 1 {
		collect(fmt.Errorf("DB_MAX_CONNS must be >= 1, got %d", cfg.DB.MaxConns))
	}
	if cfg.DB.MinConns < 0 {
		collect(fmt.Errorf("DB_MIN_CONNS must be >= 0, got %d", cfg.DB.MinConns))
	}
	if cfg.DB.MinConns > cfg.DB.MaxConns {
		collect(fmt.Errorf("DB_MIN_CONNS (%d) must not exceed DB_MAX_CONNS (%d)", cfg.DB.MinConns, cfg.DB.MaxConns))
	}

	cfg.Log.Level = envLevel("LOG_LEVEL", slog.LevelInfo, collect)
	cfg.Log.Format = strings.ToLower(envString("LOG_FORMAT", "json"))
	if cfg.Log.Format != "json" && cfg.Log.Format != "text" {
		collect(fmt.Errorf("LOG_FORMAT must be one of [json text], got %q", cfg.Log.Format))
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int, collect func(error)) int {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		collect(fmt.Errorf("%s must be an integer, got %q", key, raw))
		return def
	}
	return v
}

func envDuration(key string, def time.Duration, collect func(error)) time.Duration {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		collect(fmt.Errorf("%s must be a duration such as 15s or 1m, got %q", key, raw))
		return def
	}
	if v <= 0 {
		collect(fmt.Errorf("%s must be positive, got %q", key, raw))
		return def
	}
	return v
}

func envLevel(key string, def slog.Level, collect func(error)) slog.Level {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(raw)); err != nil {
		collect(fmt.Errorf("%s must be one of [debug info warn error], got %q", key, raw))
		return def
	}
	return lvl
}

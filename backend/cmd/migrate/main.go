package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"streetmap.in/backend/internal/config"
	"streetmap.in/backend/internal/database"
	"streetmap.in/backend/migrations"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if len(os.Args) != 2 || os.Args[1] != "up" {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/migrate up")
		os.Exit(2)
	}
	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	db, err := database.Open(cfg)
	if err != nil {
		return fmt.Errorf("configure database connection: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("close database pool failed", "error", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return errors.New("connect to configured database")
	}
	if err := migrations.Up(ctx, db); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	slog.Info("database migrations applied")
	return nil
}

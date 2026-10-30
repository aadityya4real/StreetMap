package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"streetmap.in/backend/internal/config"
	"streetmap.in/backend/internal/database"
	"streetmap.in/backend/internal/devseed"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	confirm := flag.Bool("confirm-local-seed", false, "confirm that fictional StreetMap seed data should be written to the validated local development/test database")
	remove := flag.Bool("remove", false, "remove only the fixed StreetMap seed records; refuses if user data references them")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/seed --confirm-local-seed [--remove]")
		os.Exit(2)
	}
	if !*confirm {
		slog.Error("refusing to seed without --confirm-local-seed")
		os.Exit(2)
	}
	if err := run(*remove); err != nil {
		slog.Error("local seed operation failed", "error", err)
		os.Exit(1)
	}
}

func run(remove bool) error {
	environment := os.Getenv("STREETMAP_ENV")
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	target, err := devseed.ValidateTarget(environment, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	db, err := database.Open(cfg)
	if err != nil {
		return errors.New("configure database connection")
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("close database connection failed", "error", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return errors.New("connect to validated local database")
	}
	var currentDatabase string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&currentDatabase); err != nil {
		return errors.New("verify connected database")
	}
	if currentDatabase != target.Database {
		return errors.New("connected database does not match the validated local target")
	}
	var migrationVersion int64
	if err := db.QueryRowContext(ctx, `SELECT version_id FROM goose_db_version ORDER BY id DESC LIMIT 1`).Scan(&migrationVersion); err != nil {
		return errors.New("read database migration version; apply the documented migrations first")
	}
	if migrationVersion < 2 {
		return errors.New("database migration version 2 or newer is required")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("begin seed transaction")
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if remove {
		err = devseed.Remove(ctx, tx)
	} else {
		err = devseed.Seed(ctx, tx)
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errors.New("commit seed transaction")
	}
	operation := "seeded or verified"
	if remove {
		operation = "removed"
	}
	slog.Info("local StreetMap demo data operation completed", "operation", operation, "environment", target.Environment, "database", target.Database, "host", target.Host, "seed_version", "v1")
	return nil
}

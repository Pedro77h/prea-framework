package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"time"

	"prea/common"
	"prea/core"
	"prea/internal/preas"
	"prea/logger"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		logger.Error("prea: startup failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	db, err := connect(common.GetEnv("PGCONN"))
	if err != nil {
		return err
	}
	// Runs after the server finished its in-flight requests (App.Run returns
	// only after a graceful shutdown).
	defer db.Close()

	app := core.App{
		Modules:     preas.Modules,
		Security:    preas.Security,
		Server:      preas.Server,
		Middlewares: preas.Middlewares,
		Globals:     []any{db},
	}
	return app.Run(":" + cmp.Or(os.Getenv("PORT"), "8000"))
}

// connect fails at startup when the database is unreachable; pgxpool.New is lazy.
func connect(dsn string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: %w", err)
	}
	return db, nil
}

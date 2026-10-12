package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"prea/config"
	"prea/core"
	"prea/internal/preas"
	"prea/logger"
	"prea/security"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		logger.Error("prea: startup failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("internal/config", "")
	if err != nil {
		return err
	}
	// The keys are in internal/config/application.yaml; missing ones stay "".
	var port, mode, logLevel, logFormat, dbURL, jwtSecret string
	if err := errors.Join(
		cfg.Value("server.port", &port),
		cfg.Value("server.mode", &mode),
		cfg.Value("logging.level", &logLevel),
		cfg.Value("logging.format", &logFormat),
		cfg.Value("database.url", &dbURL),
		cfg.Value("security.jwt.secret", &jwtSecret),
	); err != nil {
		return err
	}
	if err := errors.Join(logger.Configure(logLevel, logFormat), security.SetSignKey(jwtSecret)); err != nil {
		return err
	}
	gin.SetMode(mode)
	logger.Info("prea: config loaded", "profile", cfg.Profile())

	db, err := connect(dbURL)
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
		Config:      cfg,
	}
	return app.Run(":" + cmp.Or(port, "8000"))
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

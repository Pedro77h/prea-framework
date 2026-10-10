package main

import (
	"cmp"
	"context"
	"log"
	"os"

	"prea/common"
	"prea/core"
	"prea/internal/preas"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	db, err := pgxpool.New(context.Background(), common.GetEnv("PGCONN"))
	if err != nil {
		return err
	}
	defer db.Close()

	app := core.App{
		Modules:     preas.Modules,
		Middlewares: preas.Middlewares,
		Globals:     []any{db},
	}
	return app.Run(":" + cmp.Or(os.Getenv("PORT"), "8000"))
}

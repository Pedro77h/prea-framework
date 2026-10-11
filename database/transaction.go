package database

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Transactional is the @Transactional of Preá: embed it in a component and
// wrap the methods that must be atomic with Transaction.
//
//	type Service struct {
//		database.Transactional
//		Users *Repository `inject:""`
//	}
//
//	func (s *Service) Register(ctx context.Context, in dtos.Create) error {
//		return s.Transaction(ctx, func(ctx context.Context) error {
//			... every repository call with this ctx runs in the transaction
//		})
//	}
type Transactional struct {
	DB *pgxpool.Pool `inject:""`
}

type txKey struct{ db *pgxpool.Pool }

// Transaction runs fn in a transaction carried by ctx: every Statement on the
// same pool that runs with that ctx (BaseRepository included) uses it. It
// commits when fn returns nil and rolls back on an error or a panic.
//
// Called inside another transaction, it opens a savepoint: an error rolls
// back only fn's work, and the outer transaction decides what to do with it.
// A transaction is one connection: don't share its ctx between goroutines.
func (t Transactional) Transaction(ctx context.Context, fn func(ctx context.Context) error) error {
	var db interface {
		Begin(context.Context) (pgx.Tx, error)
	} = t.DB
	if tx, ok := ctx.Value(txKey{t.DB}).(pgx.Tx); ok {
		db = tx
	}
	return translate(pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		return fn(context.WithValue(ctx, txKey{t.DB}, tx))
	}))
}

// connFor returns the transaction ctx carries for conn's pool, or conn itself.
func connFor(ctx context.Context, conn Conn) Conn {
	if pool, ok := conn.(*pgxpool.Pool); ok {
		if tx, ok := ctx.Value(txKey{pool}).(pgx.Tx); ok {
			return tx
		}
	}
	return conn
}

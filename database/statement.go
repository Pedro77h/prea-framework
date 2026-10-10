package database

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"prea/common"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Conn is satisfied by *pgxpool.Pool, *pgx.Conn and pgx.Tx.
type Conn interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Args are named parameters, referenced as @name in SQL.
type Args map[string]any

// Statement is a query with named parameters, PDO style:
//
//	users, err := database.Prepare[User](conn, "select * from users where email = @email").
//		Bind("email", email).
//		All(ctx)
type Statement[T any] struct {
	conn Conn
	sql  string
	args pgx.NamedArgs
}

func Prepare[T any](conn Conn, sql string) *Statement[T] {
	return &Statement[T]{conn: conn, sql: sql, args: pgx.NamedArgs{}}
}

func (s *Statement[T]) Bind(name string, value any) *Statement[T] {
	s.args[name] = value
	return s
}

func (s *Statement[T]) BindArgs(args Args) *Statement[T] {
	for name, value := range args {
		s.args[name] = value
	}
	return s
}

// All returns every row. T is a struct (columns map to fields by `db` tag or
// name, ignoring case and underscores) or a scalar for single-column queries.
func (s *Statement[T]) All(ctx context.Context) ([]T, error) {
	rows, err := s.conn.Query(ctx, s.sql, s.args)
	if err != nil {
		return nil, translate(err)
	}
	items, err := pgx.CollectRows(rows, rowMapper[T]())
	return items, translate(err)
}

// One returns exactly one row, or common.ErrNotFound.
func (s *Statement[T]) One(ctx context.Context) (T, error) {
	rows, err := s.conn.Query(ctx, s.sql, s.args)
	if err != nil {
		var zero T
		return zero, translate(err)
	}
	item, err := pgx.CollectExactlyOneRow(rows, rowMapper[T]())
	return item, translate(err)
}

// Exec runs a statement that returns no rows and reports the affected rows.
func (s *Statement[T]) Exec(ctx context.Context) (int64, error) {
	tag, err := s.conn.Exec(ctx, s.sql, s.args)
	return tag.RowsAffected(), translate(err)
}

// param binds value under a generated name and returns its placeholder.
func (s *Statement[T]) param(i int, value any) string {
	name := fmt.Sprintf("p%d", i)
	s.args[name] = value
	return "@" + name
}

var timeType = reflect.TypeFor[time.Time]()

func rowMapper[T any]() pgx.RowToFunc[T] {
	t := reflect.TypeFor[T]()
	if t.Kind() == reflect.Struct && t != timeType {
		return pgx.RowToStructByNameLax[T]
	}
	return pgx.RowTo[T]
}

func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return common.ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", common.ErrConflict, pgErr.Message)
		case "22P02", "23502", "23503", "23514": // bad text representation, not null, foreign key, check
			return fmt.Errorf("%w: %s", common.ErrInvalidInput, pgErr.Message)
		}
	}
	return err
}

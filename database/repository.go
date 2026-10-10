package database

import (
	"context"
	"fmt"
	"strings"

	"prea/common"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const PrimaryKey = "id"

type Model interface {
	Table() string
}

// BaseRepository gives a model the basic CRUD. Embed it in a module repository
// and write custom queries with r.Prepare.
//
// Create inserts every field of data; Update only the non-zero ones
// (pass Args to set a zero value explicitly).
type BaseRepository[T Model] struct {
	DB *pgxpool.Pool `inject:""`
}

// Prepare starts a custom query whose rows map to T.
func (r *BaseRepository[T]) Prepare(sql string) *Statement[T] {
	return Prepare[T](r.DB, sql)
}

func (r *BaseRepository[T]) GetAll(ctx context.Context) ([]T, error) {
	return r.Prepare("select * from " + r.table() + " order by " + PrimaryKey).All(ctx)
}

func (r *BaseRepository[T]) GetByID(ctx context.Context, id any) (T, error) {
	return r.Prepare("select * from "+r.table()+" where "+PrimaryKey+" = @id").Bind("id", id).One(ctx)
}

// Where returns the rows matching every column = value of filter (nil means IS NULL).
func (r *BaseRepository[T]) Where(ctx context.Context, filter Args) ([]T, error) {
	stmt, err := r.selectWhere(filter, "")
	if err != nil {
		return nil, err
	}
	return stmt.All(ctx)
}

// First returns the first row matching filter, or common.ErrNotFound.
func (r *BaseRepository[T]) First(ctx context.Context, filter Args) (T, error) {
	stmt, err := r.selectWhere(filter, " limit 1")
	if err != nil {
		var zero T
		return zero, err
	}
	return stmt.One(ctx)
}

func (r *BaseRepository[T]) Create(ctx context.Context, data any) (T, error) {
	var zero T
	cols, vals, err := columns(data, false)
	if err != nil {
		return zero, err
	}
	cols, vals = withoutZeroPrimaryKey(cols, vals)
	if len(cols) == 0 {
		return zero, fmt.Errorf("%w: nothing to insert", common.ErrInvalidInput)
	}

	stmt := r.Prepare("")
	names := make([]string, len(cols))
	params := make([]string, len(cols))
	for i, col := range cols {
		names[i] = quote(col)
		params[i] = stmt.param(i, vals[i])
	}
	stmt.sql = fmt.Sprintf("insert into %s (%s) values (%s) returning *",
		r.table(), strings.Join(names, ", "), strings.Join(params, ", "))
	return stmt.One(ctx)
}

func (r *BaseRepository[T]) Update(ctx context.Context, id any, data any) (T, error) {
	var zero T
	cols, vals, err := columns(data, true)
	if err != nil {
		return zero, err
	}
	if len(cols) == 0 {
		return zero, fmt.Errorf("%w: nothing to update", common.ErrInvalidInput)
	}

	stmt := r.Prepare("").Bind("id", id)
	sets := make([]string, len(cols))
	for i, col := range cols {
		sets[i] = quote(col) + " = " + stmt.param(i, vals[i])
	}
	stmt.sql = fmt.Sprintf("update %s set %s where %s = @id returning *",
		r.table(), strings.Join(sets, ", "), PrimaryKey)
	return stmt.One(ctx)
}

func (r *BaseRepository[T]) Delete(ctx context.Context, id any) error {
	n, err := r.Prepare("delete from "+r.table()+" where "+PrimaryKey+" = @id").Bind("id", id).Exec(ctx)
	if err == nil && n == 0 {
		return common.ErrNotFound
	}
	return err
}

func (r *BaseRepository[T]) selectWhere(filter Args, suffix string) (*Statement[T], error) {
	if len(filter) == 0 {
		return nil, fmt.Errorf("%w: empty filter", common.ErrInvalidInput)
	}
	cols, vals, _ := columns(filter, false)

	stmt := r.Prepare("")
	conds := make([]string, len(cols))
	for i, col := range cols {
		if vals[i] == nil {
			conds[i] = quote(col) + " is null"
		} else {
			conds[i] = quote(col) + " = " + stmt.param(i, vals[i])
		}
	}
	stmt.sql = fmt.Sprintf("select * from %s where %s order by %s%s",
		r.table(), strings.Join(conds, " and "), PrimaryKey, suffix)
	return stmt, nil
}

func (r *BaseRepository[T]) table() string {
	var model T
	return pgx.Identifier(strings.Split(model.Table(), ".")).Sanitize()
}

func quote(col string) string {
	return pgx.Identifier{col}.Sanitize()
}

func withoutZeroPrimaryKey(cols []string, vals []any) ([]string, []any) {
	for i, col := range cols {
		if col == PrimaryKey && isZero(vals[i]) {
			return append(cols[:i:i], cols[i+1:]...), append(vals[:i:i], vals[i+1:]...)
		}
	}
	return cols, vals
}

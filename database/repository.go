package database

import (
	"context"
	"fmt"
	"slices"
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
// Create inserts every field of data. Update is a patch: it skips zero fields,
// so a DTO with pointer fields (nil = keep) or Args can set zero values. To
// write the whole entity, zeros included, use Save, like JPA's save/merge.
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

// Save writes every column of entity, zero values included: it inserts when
// the primary key is zero and updates the row otherwise (ErrNotFound if it
// doesn't exist). Only the key gets its column default on insert; for the
// others, use Create with a DTO. Load, change and save, inside a Transaction when the read
// and the write must be atomic.
func (r *BaseRepository[T]) Save(ctx context.Context, entity T) (T, error) {
	cols, vals, err := columns(entity, false)
	if err != nil {
		return entity, err
	}
	i := slices.Index(cols, PrimaryKey)
	if i < 0 || isZero(vals[i]) {
		return r.Create(ctx, entity)
	}

	stmt := r.Prepare("").Bind("id", vals[i])
	sets := make([]string, 0, len(cols)-1)
	for j, col := range cols {
		if j != i {
			sets = append(sets, quote(col)+" = "+stmt.param(j, vals[j]))
		}
	}
	stmt.sql = fmt.Sprintf("update %s set %s where %s = @id returning *",
		r.table(), strings.Join(sets, ", "), PrimaryKey)
	return stmt.One(ctx)
}

// Count returns how many rows match filter; an empty filter counts them all.
func (r *BaseRepository[T]) Count(ctx context.Context, filter Args) (int64, error) {
	stmt := Prepare[int64](r.DB, "")
	stmt.sql = "select count(*) from " + r.table() + where(stmt.param, filter)
	return stmt.One(ctx)
}

// Exists reports whether any row matches filter; an empty filter checks the table.
func (r *BaseRepository[T]) Exists(ctx context.Context, filter Args) (bool, error) {
	stmt := Prepare[bool](r.DB, "")
	stmt.sql = "select exists (select 1 from " + r.table() + where(stmt.param, filter) + ")"
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
	stmt := r.Prepare("")
	stmt.sql = fmt.Sprintf("select * from %s%s order by %s%s", r.table(), where(stmt.param, filter), PrimaryKey, suffix)
	return stmt, nil
}

// where builds " where col = @pN and ..." (nil means IS NULL), binding the
// values with param; an empty filter gives "".
func where(param func(int, any) string, filter Args) string {
	if len(filter) == 0 {
		return ""
	}
	cols, vals, _ := columns(filter, false)
	conds := make([]string, len(cols))
	for i, col := range cols {
		if vals[i] == nil {
			conds[i] = quote(col) + " is null"
		} else {
			conds[i] = quote(col) + " = " + param(i, vals[i])
		}
	}
	return " where " + strings.Join(conds, " and ")
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

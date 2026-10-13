package database

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Model interface {
	Table() string
}

// BaseRepository gives a model the basic CRUD. Embed it in a module repository
// and write custom queries with r.Prepare. The primary key, the generated
// columns and the version come from the model's tags (see the package doc).
//
// Create inserts every field of data. Update is a patch: it skips zero fields,
// so a DTO with pointer fields (nil = keep) or Args can set zero values. To
// write the whole entity, zeros included, use Save, like JPA's save/merge.
//
// The id of GetByID, Update and Delete is the key value; with a composite
// key, it is Args with every key column.
type BaseRepository[T Model] struct {
	DB *pgxpool.Pool `inject:""`
}

// Prepare starts a custom query whose rows map to T.
func (r *BaseRepository[T]) Prepare(sql string) *Statement[T] {
	return Prepare[T](r.DB, sql)
}

// query builds a statement from the model's schema; a schema or build error
// is returned when the statement runs.
func (r *BaseRepository[T]) query(build func(t *table, s *Statement[T]) (string, error)) *Statement[T] {
	s := r.Prepare("")
	t, err := schemaOf(reflect.TypeFor[T]())
	if err == nil {
		s.sql, err = build(t, s)
	}
	s.err = err
	return s
}

func (r *BaseRepository[T]) GetAll(ctx context.Context) ([]T, error) {
	return r.query(func(t *table, s *Statement[T]) (string, error) {
		return "select * from " + t.quoted() + t.orderBy(), nil
	}).All(ctx)
}

func (r *BaseRepository[T]) GetByID(ctx context.Context, id any) (T, error) {
	return r.query(func(t *table, s *Statement[T]) (string, error) {
		cond, err := t.key(s.param, id)
		return "select * from " + t.quoted() + " where " + cond, err
	}).One(ctx)
}

// Where returns the rows matching every column = value of filter (nil means IS NULL).
func (r *BaseRepository[T]) Where(ctx context.Context, filter Args) ([]T, error) {
	return r.selectWhere(filter, "").All(ctx)
}

// First returns the first row matching filter, or ErrNotFound.
func (r *BaseRepository[T]) First(ctx context.Context, filter Args) (T, error) {
	return r.selectWhere(filter, " limit 1").One(ctx)
}

func (r *BaseRepository[T]) Create(ctx context.Context, data any) (T, error) {
	return r.query(func(t *table, s *Statement[T]) (string, error) {
		cols, vals, err := columns(data, false)
		if err != nil {
			return "", err
		}
		return t.insert(s.param, cols, vals)
	}).One(ctx)
}

// Update patches the row with the non-zero fields of data; a version column
// is incremented.
func (r *BaseRepository[T]) Update(ctx context.Context, id any, data any) (T, error) {
	return r.query(func(t *table, s *Statement[T]) (string, error) {
		cols, vals, err := columns(data, true)
		if err != nil {
			return "", err
		}
		v, versioned := t.version()
		var sets []string
		for i, col := range cols {
			if !versioned || col != v.name {
				sets = append(sets, quote(col)+" = "+s.param(vals[i]))
			}
		}
		if len(sets) == 0 {
			return "", fmt.Errorf("%w: nothing to update", ErrInvalidInput)
		}
		if versioned {
			sets = append(sets, quote(v.name)+" = "+quote(v.name)+" + 1")
		}
		cond, err := t.key(s.param, id)
		return fmt.Sprintf("update %s set %s where %s returning *", t.quoted(), strings.Join(sets, ", "), cond), err
	}).One(ctx)
}

// Save writes every column of entity, zero values included, like JPA's save:
//   - a zero key inserts, and only the key gets its column default (for the
//     others, use Create with a DTO);
//   - a generated key updates the row, or returns ErrNotFound;
//   - a natural key (not generated, composite included) inserts or updates
//     the row (upsert).
//
// With a version column, the write only happens if the row still has the
// entity's version, which is incremented; otherwise it returns ErrConflict.
// Load, change and save, inside a Transaction when the read and the write
// must be atomic.
func (r *BaseRepository[T]) Save(ctx context.Context, entity T) (T, error) {
	t, err := schemaOf(reflect.TypeFor[T]())
	if err != nil {
		return entity, err
	}
	cols, vals, err := columns(entity, false)
	if err != nil {
		return entity, err
	}
	key := Args{}
	for _, c := range t.pk() {
		key[c.name] = vals[slices.Index(cols, c.name)]
	}
	if slices.ContainsFunc(t.pk(), func(c column) bool { return isZero(key[c.name]) }) {
		return r.Create(ctx, entity)
	}
	generated := slices.ContainsFunc(t.pk(), func(c column) bool { return c.generated != "" })

	saved, err := r.query(func(t *table, s *Statement[T]) (string, error) {
		if generated {
			return t.update(s.param, cols, vals)
		}
		return t.upsert(s.param, cols, vals)
	}).One(ctx)
	if _, versioned := t.version(); versioned && errors.Is(err, ErrNotFound) {
		if generated {
			if exists, err := r.Exists(ctx, key); err != nil || !exists {
				return saved, cmp.Or(err, ErrNotFound)
			}
		}
		return saved, fmt.Errorf("%w: %s changed since it was read", ErrConflict, t.name)
	}
	return saved, err
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
	n, err := r.query(func(t *table, s *Statement[T]) (string, error) {
		cond, err := t.key(s.param, id)
		return "delete from " + t.quoted() + " where " + cond, err
	}).Exec(ctx)
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

func (r *BaseRepository[T]) selectWhere(filter Args, suffix string) *Statement[T] {
	return r.query(func(t *table, s *Statement[T]) (string, error) {
		if len(filter) == 0 {
			return "", fmt.Errorf("%w: empty filter", ErrInvalidInput)
		}
		return "select * from " + t.quoted() + where(s.param, filter) + t.orderBy() + suffix, nil
	})
}

// where builds " where col = @pN and ..." (nil means IS NULL), binding the
// values with param; an empty filter gives "".
func where(param func(any) string, filter Args) string {
	if len(filter) == 0 {
		return ""
	}
	cols, vals, _ := columns(filter, false)
	conds := make([]string, len(cols))
	for i, col := range cols {
		if vals[i] == nil {
			conds[i] = quote(col) + " is null"
		} else {
			conds[i] = quote(col) + " = " + param(vals[i])
		}
	}
	return " where " + strings.Join(conds, " and ")
}

// key builds the condition on the primary key: id is the value, or Args with
// every column of a composite key.
func (t *table) key(param func(any) string, id any) (string, error) {
	pk := t.pk()
	if len(pk) == 1 {
		return quote(pk[0].name) + " = " + param(id), nil
	}
	names := make([]string, len(pk))
	for i, c := range pk {
		names[i] = c.name
	}
	args, ok := id.(Args)
	if !ok || len(args) != len(pk) {
		return "", fmt.Errorf("%w: %s has a composite key: pass Args with %s", ErrInvalidInput, t.name, strings.Join(names, ", "))
	}
	conds := make([]string, len(pk))
	for i, name := range names {
		v, ok := args[name]
		if !ok {
			return "", fmt.Errorf("%w: key column %s is missing", ErrInvalidInput, name)
		}
		conds[i] = quote(name) + " = " + param(v)
	}
	return strings.Join(conds, " and "), nil
}

func (t *table) orderBy() string {
	names := make([]string, 0, 1)
	for _, c := range t.pk() {
		names = append(names, quote(c.name))
	}
	return " order by " + strings.Join(names, ", ")
}

// insert skips the zero key columns, so the database fills them.
func (t *table) insert(param func(any) string, cols []string, vals []any) (string, error) {
	var names, params []string
	for i, col := range cols {
		if t.isKey(col) && isZero(vals[i]) {
			continue
		}
		names = append(names, quote(col))
		params = append(params, param(vals[i]))
	}
	if len(names) == 0 {
		return "", fmt.Errorf("%w: nothing to insert", ErrInvalidInput)
	}
	return fmt.Sprintf("insert into %s (%s) values (%s) returning *",
		t.quoted(), strings.Join(names, ", "), strings.Join(params, ", ")), nil
}

// update writes every non key column of the row with that key, checking and
// incrementing the version.
func (t *table) update(param func(any) string, cols []string, vals []any) (string, error) {
	v, versioned := t.version()
	var sets, conds []string
	for i, col := range cols {
		switch {
		case t.isKey(col):
			conds = append(conds, quote(col)+" = "+param(vals[i]))
		case versioned && col == v.name:
			conds = append(conds, quote(col)+" = "+param(vals[i]))
			sets = append(sets, quote(col)+" = "+quote(col)+" + 1")
		default:
			sets = append(sets, quote(col)+" = "+param(vals[i]))
		}
	}
	if len(sets) == 0 {
		return "", fmt.Errorf("%w: nothing to update", ErrInvalidInput)
	}
	return fmt.Sprintf("update %s set %s where %s returning *",
		t.quoted(), strings.Join(sets, ", "), strings.Join(conds, " and ")), nil
}

// upsert inserts the row or, when the key exists, updates every other column;
// with a version, only if it still matches (no row comes back otherwise).
func (t *table) upsert(param func(any) string, cols []string, vals []any) (string, error) {
	v, versioned := t.version()
	var names, params, keys, sets []string
	for i, col := range cols {
		names = append(names, quote(col))
		params = append(params, param(vals[i]))
		switch {
		case t.isKey(col):
			keys = append(keys, quote(col))
		case versioned && col == v.name:
			sets = append(sets, quote(col)+" = t."+quote(col)+" + 1")
		default:
			sets = append(sets, quote(col)+" = excluded."+quote(col))
		}
	}
	if len(sets) == 0 { // only key columns: a no-op update still returns the row
		sets = append(sets, keys[0]+" = excluded."+keys[0])
	}
	sql := fmt.Sprintf("insert into %s as t (%s) values (%s) on conflict (%s) do update set %s",
		t.quoted(), strings.Join(names, ", "), strings.Join(params, ", "), strings.Join(keys, ", "), strings.Join(sets, ", "))
	if versioned {
		sql += " where t." + quote(v.name) + " = excluded." + quote(v.name)
	}
	return sql + " returning *", nil
}

func (t *table) isKey(col string) bool {
	return slices.ContainsFunc(t.pk(), func(c column) bool { return c.name == col })
}

func (r *BaseRepository[T]) table() string {
	var model T
	return quoteTable(model.Table())
}

// quoteTable quotes "schema.table" as "schema"."table".
func quoteTable(name string) string {
	return pgx.Identifier(strings.Split(name, ".")).Sanitize()
}

func quote(col string) string {
	return pgx.Identifier{col}.Sanitize()
}

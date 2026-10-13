package database

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Schema modes, like Hibernate's ddl-auto (config key database.schema).
const (
	SchemaNone       = "none"        // only checks the models' tags
	SchemaValidate   = "validate"    // the tables must match the models, or the app doesn't start
	SchemaUpdate     = "update"      // creates the missing tables, columns and indexes; never drops
	SchemaCreate     = "create"      // drops the models' tables and creates them
	SchemaCreateDrop = "create-drop" // create, and drop them on shutdown
)

// repository is implemented by every component that embeds a BaseRepository.
type repository interface {
	model() (*pgxpool.Pool, reflect.Type)
}

func (r *BaseRepository[T]) model() (*pgxpool.Pool, reflect.Type) {
	return r.DB, reflect.TypeFor[T]()
}

// SyncSchema reads the models of the repositories among components and
// applies mode to their database ("" is none). Invalid tags fail in every
// mode. drop removes the tables under create-drop and does nothing otherwise;
// call it on shutdown.
func SyncSchema(ctx context.Context, mode string, components []any) (drop func(context.Context) error, err error) {
	drop = func(context.Context) error { return nil }
	mode = cmp.Or(mode, SchemaNone)
	if !slices.Contains([]string{SchemaNone, SchemaValidate, SchemaUpdate, SchemaCreate, SchemaCreateDrop}, mode) {
		return drop, fmt.Errorf("database: schema %q is not none, validate, update, create or create-drop", mode)
	}

	var pools []*pgxpool.Pool
	tables := map[*pgxpool.Pool][]*table{}
	byName := map[string]*table{}
	var errs []error
	for _, component := range components {
		r, ok := component.(repository)
		if !ok {
			continue
		}
		db, typ := r.model()
		if db == nil && mode != SchemaNone {
			errs = append(errs, fmt.Errorf("database: %T has no database (DB is nil)", component))
			continue
		}
		t, err := schemaOf(typ)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if prev, ok := byName[t.name]; ok {
			if prev.typ != typ {
				errs = append(errs, fmt.Errorf("database: table %s is mapped by %v and %v", t.name, prev.typ, typ))
			}
			continue
		}
		byName[t.name] = t
		if _, ok := tables[db]; !ok {
			pools = append(pools, db)
		}
		tables[db] = append(tables[db], t)
	}
	for _, db := range pools {
		sorted, err := ordered(tables[db])
		if err != nil {
			errs = append(errs, err)
		}
		tables[db] = sorted
	}
	if err := errors.Join(errs...); err != nil || mode == SchemaNone {
		return drop, err
	}

	for _, db := range pools {
		switch mode {
		case SchemaValidate:
			errs = append(errs, validate(ctx, db, tables[db]))
		case SchemaUpdate:
			errs = append(errs, exec(ctx, db, ddl(tables[db])))
		case SchemaCreate, SchemaCreateDrop:
			errs = append(errs, exec(ctx, db, slices.Concat(drops(tables[db]), ddl(tables[db]))))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return drop, err
	}
	if mode == SchemaCreateDrop {
		drop = func(ctx context.Context) error {
			var errs []error
			for _, db := range pools {
				errs = append(errs, exec(ctx, db, drops(tables[db])))
			}
			return errors.Join(errs...)
		}
	}
	return drop, nil
}

// exec runs stmts in one transaction: all of them or none.
func exec(ctx context.Context, db *pgxpool.Pool, stmts []string) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		for _, stmt := range stmts {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("database: %s: %w", stmt, err)
			}
		}
		return nil
	})
}

// validate compares the tables with their models and reports every
// difference at once: a missing table or column, a type or a nullability.
func validate(ctx context.Context, db *pgxpool.Pool, tables []*table) error {
	type dbColumn struct {
		Type     string
		Size     int
		Nullable bool
	}
	var errs []error
	for _, t := range tables {
		rows, err := db.Query(ctx, `
			select a.attname, ty.typname,
				case when ty.typname in ('varchar', 'bpchar') and a.atttypmod > 4 then a.atttypmod - 4 else 0 end,
				not a.attnotnull
			from pg_attribute a join pg_type ty on ty.oid = a.atttypid
			where a.attrelid = to_regclass($1) and a.attnum > 0 and not a.attisdropped`, t.quoted())
		if err != nil {
			return fmt.Errorf("database: reading %s: %w", t.name, err)
		}
		found := map[string]dbColumn{}
		var name string
		var col dbColumn
		_, err = pgx.ForEachRow(rows, []any{&name, &col.Type, &col.Size, &col.Nullable}, func() error {
			found[name] = col
			return nil
		})
		if err != nil {
			return fmt.Errorf("database: reading %s: %w", t.name, err)
		}
		if len(found) == 0 {
			errs = append(errs, fmt.Errorf("table %s is missing", t.name))
			continue
		}
		for _, c := range t.columns {
			got, ok := found[c.name]
			switch {
			case !ok:
				errs = append(errs, fmt.Errorf("%s.%s is missing", t.name, c.name))
			case got.Type != c.sqlType || got.Size != c.size:
				errs = append(errs, fmt.Errorf("%s.%s is %s, the model wants %s", t.name, c.name, typeName(got.Type, got.Size), typeName(c.sqlType, c.size)))
			case got.Nullable && !c.nullable:
				errs = append(errs, fmt.Errorf("%s.%s is nullable: make the field a pointer, or the column not null", t.name, c.name))
			case !got.Nullable && c.nullable:
				errs = append(errs, fmt.Errorf("%s.%s is not null: make the field a value, or the column nullable", t.name, c.name))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("database: the schema doesn't match the models:\n%w", errors.Join(errs...))
	}
	return nil
}

func typeName(typ string, size int) string {
	if size > 0 {
		return typ + "(" + strconv.Itoa(size) + ")"
	}
	return typ
}
